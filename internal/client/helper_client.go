// Package client provides the client for communicating with the helper daemon.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/shini4i/openfortivpn-gui/internal/helper/protocol"
	"github.com/shini4i/openfortivpn-gui/internal/helper/server"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

const (
	// DefaultTimeout for RPC calls.
	DefaultTimeout = 30 * time.Second
)

// ErrHelperNotAvailable is returned when the helper daemon is not running.
var ErrHelperNotAvailable = errors.New("helper daemon not available")

// HelperClient implements vpn.VPNController by communicating with the helper daemon.
type HelperClient struct {
	socketPath string
	conn       net.Conn
	reader     *bufio.Reader

	mu            sync.RWMutex
	state         vpn.ConnectionState
	assignedIP    string
	interfaceName string
	onStateChange func(old, new vpn.ConnectionState)
	onOutput      func(line string)
	onEvent       func(event *vpn.OutputEvent)
	onError       func(err error)

	// writeMu serializes NDJSON writes to prevent interleaved JSON lines
	writeMu sync.Mutex

	// Pending requests waiting for responses
	pendingMu sync.Mutex
	pending   map[string]chan *protocol.Response

	// Close channel
	closeChan chan struct{}
	closeOnce sync.Once
}

// NewHelperClient creates a new client connected to the helper daemon.
func NewHelperClient() (*HelperClient, error) {
	return NewHelperClientWithPath(server.DefaultSocketPath)
}

// NewHelperClientWithPath creates a new client connected to the helper daemon at the given path.
func NewHelperClientWithPath(socketPath string) (*HelperClient, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHelperNotAvailable, err)
	}

	client := &HelperClient{
		socketPath: socketPath,
		conn:       conn,
		reader:     bufio.NewReader(conn),
		state:      vpn.StateDisconnected,
		pending:    make(map[string]chan *protocol.Response),
		closeChan:  make(chan struct{}),
	}

	// Start event reader goroutine
	go client.readLoop()

	// Sync initial state
	if err := client.syncState(); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			slog.Warn("Failed to close client after sync error", "error", closeErr)
		}
		return nil, err
	}

	return client, nil
}

// IsHelperAvailable checks if the helper daemon is available.
func IsHelperAvailable() bool {
	return IsHelperAvailableAt(server.DefaultSocketPath)
}

// IsHelperAvailableAt checks if the helper daemon is available at the given path.
func IsHelperAvailableAt(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	_ = conn.Close() // Error intentionally ignored; we only check connectivity
	return true
}

// Close closes the connection to the helper daemon.
func (c *HelperClient) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		close(c.closeChan)
		if c.conn != nil {
			closeErr = c.conn.Close()
		}
	})
	return closeErr
}

// GetState returns the current connection state.
func (c *HelperClient) GetState() vpn.ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// GetAssignedIP returns the IP address assigned by the VPN server.
func (c *HelperClient) GetAssignedIP() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.assignedIP
}

// GetInterface returns the network interface name used by the VPN tunnel.
func (c *HelperClient) GetInterface() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.interfaceName
}

// detectInterface attempts to detect the VPN interface by the assigned IP.
// It retries with exponential backoff since the interface may take a moment to appear.
func (c *HelperClient) detectInterface(assignedIP string) {
	const maxRetries = 5
	backoff := 100 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		ifaceName, err := vpn.DetectVPNInterface(assignedIP)
		if err == nil {
			c.mu.Lock()
			c.interfaceName = ifaceName
			c.mu.Unlock()
			slog.Info("Detected VPN interface", "interface", ifaceName, "ip", assignedIP)
			return
		}

		time.Sleep(backoff)
		backoff *= 2
	}

	slog.Warn("Failed to detect VPN interface after retries", "ip", assignedIP)
}

// CanConnect returns true if a connection can be initiated.
func (c *HelperClient) CanConnect() bool {
	return c.GetState().CanConnect()
}

// CanDisconnect returns true if a disconnection can be initiated.
func (c *HelperClient) CanDisconnect() bool {
	return c.GetState().CanDisconnect()
}

// Connect initiates a VPN connection.
func (c *HelperClient) Connect(ctx context.Context, p *profile.Profile, opts *vpn.ConnectOptions) error {
	if opts == nil {
		opts = &vpn.ConnectOptions{}
	}

	params := protocol.ConnectParams{
		ProfileID:          p.ID,
		Host:               p.Host,
		Port:               p.Port,
		Username:           p.Username,
		Password:           opts.Password,
		OTP:                opts.OTP,
		AuthMethod:         string(p.AuthMethod),
		Realm:              p.Realm,
		TrustedCert:        p.TrustedCert,
		ClientCertPath:     p.ClientCertPath,
		ClientKeyPath:      p.ClientKeyPath,
		SetDNS:             p.SetDNS,
		SetRoutes:          p.SetRoutes,
		HalfInternetRoutes: p.HalfInternetRoutes,
	}

	_, err := c.sendRequest(ctx, protocol.CommandConnect, params)
	return err
}

// Disconnect terminates the active VPN connection.
// If ctx is nil, a default timeout context will be used.
func (c *HelperClient) Disconnect(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	_, err := c.sendRequest(ctx, protocol.CommandDisconnect, protocol.DisconnectParams{})
	return err
}

// OnStateChange registers a callback for state changes.
func (c *HelperClient) OnStateChange(callback func(old, new vpn.ConnectionState)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onStateChange = callback
}

// OnOutput registers a callback for output lines.
func (c *HelperClient) OnOutput(callback func(line string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onOutput = callback
}

// OnEvent registers a callback for VPN events.
func (c *HelperClient) OnEvent(callback func(event *vpn.OutputEvent)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvent = callback
}

// OnError registers a callback for errors.
func (c *HelperClient) OnError(callback func(err error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onError = callback
}

func (c *HelperClient) syncState() error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	resp, err := c.sendRequest(ctx, protocol.CommandStatus, protocol.StatusParams{})
	if err != nil {
		return err
	}

	var status protocol.StatusResult
	if err := json.Unmarshal(resp.Result, &status); err != nil {
		return fmt.Errorf("failed to parse status: %w", err)
	}

	c.mu.Lock()
	c.state = vpn.ConnectionState(status.State)
	c.assignedIP = status.AssignedIP
	c.mu.Unlock()

	return nil
}

func (c *HelperClient) sendRequest(ctx context.Context, cmd protocol.Command, params interface{}) (*protocol.Response, error) {
	id := uuid.New().String()

	req, err := protocol.NewRequest(id, cmd, params)
	if err != nil {
		return nil, err
	}

	// Create response channel
	respChan := make(chan *protocol.Response, 1)
	c.pendingMu.Lock()
	c.pending[id] = respChan
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	// Send request - serialize writes to prevent interleaved JSON lines
	c.writeMu.Lock()
	data, err := json.Marshal(req)
	if err != nil {
		c.writeMu.Unlock()
		return nil, err
	}
	data = append(data, '\n')

	_, writeErr := c.conn.Write(data)
	c.writeMu.Unlock()

	if writeErr != nil {
		return nil, fmt.Errorf("failed to send request: %w", writeErr)
	}

	// Wait for response
	select {
	case resp := <-respChan:
		if !resp.Success {
			if resp.Error != nil {
				return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
			}
			return nil, errors.New("request failed with unknown error")
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closeChan:
		return nil, errors.New("client closed")
	}
}

func (c *HelperClient) readLoop() {
	for {
		select {
		case <-c.closeChan:
			return
		default:
		}

		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF && !errors.Is(err, net.ErrClosed) {
				slog.Error("Read error from helper", "error", err)
			}
			return
		}

		c.handleMessage(line)
	}
}

func (c *HelperClient) handleMessage(data []byte) {
	// Try to determine message type
	var msg struct {
		Type protocol.MessageType `json:"type"`
		ID   string               `json:"id,omitempty"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Warn("Invalid message from helper", "error", err)
		return
	}

	switch msg.Type {
	case protocol.MessageTypeResponse:
		var resp protocol.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			slog.Warn("Invalid response from helper", "error", err)
			return
		}
		c.handleResponse(&resp)

	case protocol.MessageTypeEvent:
		var event protocol.Event
		if err := json.Unmarshal(data, &event); err != nil {
			slog.Warn("Invalid event from helper", "error", err)
			return
		}
		c.handleEvent(&event)

	default:
		// Log unknown message types for debugging (forward compatibility)
		truncatedData := string(data)
		if len(truncatedData) > 200 {
			truncatedData = truncatedData[:200] + "..."
		}
		slog.Warn("Unknown message type from helper",
			"type", msg.Type,
			"data", truncatedData)
	}
}

func (c *HelperClient) handleResponse(resp *protocol.Response) {
	c.pendingMu.Lock()
	ch, ok := c.pending[resp.ID]
	c.pendingMu.Unlock()

	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

func (c *HelperClient) handleEvent(event *protocol.Event) {
	switch event.Name {
	case protocol.EventStateChange:
		var data protocol.StateChangeData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			slog.Warn("Invalid state change event", "error", err)
			return
		}
		c.mu.Lock()
		oldState := c.state
		c.state = vpn.ConnectionState(data.To)
		// Clear interface and IP on disconnect.
		if vpn.ConnectionState(data.To) == vpn.StateDisconnected {
			c.assignedIP = ""
			c.interfaceName = ""
		}
		callback := c.onStateChange
		c.mu.Unlock()

		if callback != nil {
			callback(oldState, vpn.ConnectionState(data.To))
		}

	case protocol.EventOutput:
		var data protocol.OutputData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			slog.Warn("Invalid output event", "error", err)
			return
		}
		c.mu.RLock()
		callback := c.onOutput
		c.mu.RUnlock()

		if callback != nil {
			callback(data.Line)
		}

	case protocol.EventVPN:
		var data protocol.VPNEventData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			slog.Warn("Invalid VPN event", "error", err)
			return
		}

		// Update assigned IP if this is a got_ip event
		if data.EventType == string(vpn.EventGotIP) {
			if ip, ok := data.Data["ip"]; ok {
				c.mu.Lock()
				c.assignedIP = ip
				c.mu.Unlock()
				// Detect the interface in background since it may take a moment to appear.
				go c.detectInterface(ip)
			}
		}

		c.mu.RLock()
		callback := c.onEvent
		c.mu.RUnlock()

		if callback != nil {
			callback(&vpn.OutputEvent{
				Type:    vpn.EventType(data.EventType),
				Message: data.Message,
				Data:    data.Data,
			})
		}

	case protocol.EventError:
		var data protocol.ErrorData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			slog.Warn("Invalid error event", "error", err)
			return
		}
		c.mu.RLock()
		callback := c.onError
		c.mu.RUnlock()

		if callback != nil {
			callback(errors.New(data.Message))
		}
	}
}

// Ensure HelperClient implements VPNController interface.
var _ vpn.VPNController = (*HelperClient)(nil)
