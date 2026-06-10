// Package server provides the UNIX socket server for the helper daemon.
package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/shini4i/openfortivpn-gui/internal/helper/protocol"
)

const (
	// DefaultSocketPath is the default path for the UNIX socket.
	DefaultSocketPath = "/run/openfortivpn-gui/helper.sock"
	// DefaultSocketGroup is the group that can access the socket.
	DefaultSocketGroup = "openfortivpn-gui"

	// maxMessageSize is the maximum allowed size for a single message (64KB).
	// This prevents DoS attacks via unbounded memory allocation.
	maxMessageSize = 64 * 1024
	// initialBufferSize is the initial buffer size for reading messages (4KB).
	initialBufferSize = 4 * 1024
	// maxConcurrentClients is the maximum number of simultaneous client connections.
	// This prevents resource exhaustion attacks.
	maxConcurrentClients = 10
)

// RequestHandler is called for each incoming request. The clientID identifies
// the connection the request arrived on, so handlers can scope follow-up
// events to the requesting client. It should return a response to send back
// to the client.
type RequestHandler func(req *protocol.Request, clientID string) *protocol.Response

// Server manages client connections over a UNIX socket.
type Server struct {
	socketPath  string
	socketGroup string
	listener    net.Listener
	handler     RequestHandler

	mu            sync.RWMutex
	clients       map[string]*Client // keyed by client ID
	running       bool
	starting      bool          // Guards against TOCTOU race during Start()
	connSemaphore chan struct{} // Limits concurrent connections

	nextClientID atomic.Uint64
}

// NewServer creates a new server instance with the default socket group.
func NewServer(socketPath string, handler RequestHandler) *Server {
	return NewServerWithGroup(socketPath, DefaultSocketGroup, handler)
}

// NewServerWithGroup creates a new server instance with a custom socket group.
// Panics if handler is nil to prevent runtime panic when processing requests.
func NewServerWithGroup(socketPath, socketGroup string, handler RequestHandler) *Server {
	if handler == nil {
		panic("server: NewServerWithGroup called with nil handler")
	}
	return &Server{
		socketPath:    socketPath,
		socketGroup:   socketGroup,
		handler:       handler,
		clients:       make(map[string]*Client),
		connSemaphore: make(chan struct{}, maxConcurrentClients),
	}
}

// Start begins listening for connections.
// Returns an error if the server is already running or starting.
func (s *Server) Start() error {
	// Guard against double-start using starting flag to prevent TOCTOU race
	s.mu.Lock()
	if s.running || s.starting {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.starting = true
	s.mu.Unlock()

	// Helper to clear starting flag on error
	clearStarting := func() {
		s.mu.Lock()
		s.starting = false
		s.mu.Unlock()
	}

	// Perform filesystem/listen operations outside the lock
	// Remove existing socket file if it exists
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		clearStarting()
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		clearStarting()
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Set socket group ownership for access control
	if err := s.setSocketOwnership(); err != nil {
		if closeErr := listener.Close(); closeErr != nil {
			slog.Error("Failed to close listener after ownership error", "error", closeErr)
		}
		clearStarting()
		return fmt.Errorf("failed to set socket ownership: %w", err)
	}

	// Set socket permissions (readable/writable by owner and group)
	// #nosec G302 -- 0660 is intentional: group access required for unprivileged clients
	if err := os.Chmod(s.socketPath, 0660); err != nil {
		if closeErr := listener.Close(); closeErr != nil {
			slog.Error("Failed to close listener after chmod error", "error", closeErr)
		}
		clearStarting()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	// Finalize: set listener/running and clear starting under lock
	s.mu.Lock()
	s.listener = listener
	s.running = true
	s.starting = false
	s.mu.Unlock()

	slog.Info("Server started", "socket", s.socketPath, "group", s.socketGroup)

	go s.acceptLoop()

	return nil
}

// setSocketOwnership sets the group ownership of the socket file.
func (s *Server) setSocketOwnership() error {
	if s.socketGroup == "" {
		return nil // No group specified, keep default
	}

	grp, err := user.LookupGroup(s.socketGroup)
	if err != nil {
		return fmt.Errorf("group %q not found: %w", s.socketGroup, err)
	}

	gid, err := strconv.Atoi(grp.Gid)
	if err != nil {
		return fmt.Errorf("invalid gid %q: %w", grp.Gid, err)
	}

	// -1 means don't change owner (keep root), only change group
	if err := os.Chown(s.socketPath, -1, gid); err != nil {
		return fmt.Errorf("failed to chown socket: %w", err)
	}

	slog.Debug("Socket group ownership set", "group", s.socketGroup, "gid", gid)
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	listener := s.listener

	// Copy clients to slice while holding lock to avoid holding lock during Close() calls
	// which could block and cause deadlock with other goroutines
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.Unlock()

	// Close listener to stop accept loop
	if listener != nil {
		if err := listener.Close(); err != nil {
			slog.Error("Failed to close listener", "error", err)
		}
	}

	// Close all client connections (outside of lock to prevent deadlock)
	for _, client := range clients {
		if err := client.Close(); err != nil {
			slog.Warn("Failed to close client connection", "error", err)
		}
	}

	// Remove socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to remove socket file", "path", s.socketPath, "error", err)
	}

	slog.Info("Server stopped")
	return nil
}

// Broadcast sends an event to all connected clients.
// Clients are snapshotted before sending to avoid holding the lock during I/O.
func (s *Server) Broadcast(event *protocol.Event) {
	// Snapshot clients while holding the read lock
	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	// Send events outside the lock to avoid blocking other operations
	for _, client := range clients {
		if err := client.SendEvent(event); err != nil {
			slog.Warn("Failed to send event to client", "error", err)
		}
	}
}

// SendToClient sends an event to the single client identified by clientID.
// A client that has already disconnected is not an error — the event is
// simply dropped, since there is no longer anyone to deliver it to.
func (s *Server) SendToClient(clientID string, event *protocol.Event) error {
	s.mu.RLock()
	client, ok := s.clients[clientID]
	s.mu.RUnlock()

	if !ok {
		slog.Debug("Dropping event for disconnected client", "client", clientID)
		return nil
	}

	return client.SendEvent(event)
}

// ClientCount returns the number of connected clients.
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.RLock()
			running := s.running
			s.mu.RUnlock()
			if !running {
				return // Server is shutting down
			}
			slog.Error("Accept error", "error", err)
			continue
		}

		// Try to acquire connection semaphore (non-blocking check first)
		select {
		case s.connSemaphore <- struct{}{}:
			// Acquired semaphore, proceed with client
			clientID := fmt.Sprintf("client-%d", s.nextClientID.Add(1))
			client := newClient(clientID, conn)
			s.addClient(client)
			logPeerCredentials(clientID, conn)
			go s.handleClient(client)
		default:
			// Server at capacity, reject connection
			slog.Warn("Connection rejected: server at maximum capacity", "max", maxConcurrentClients)
			if err := conn.Close(); err != nil {
				slog.Debug("Failed to close rejected connection", "error", err)
			}
		}
	}
}

func (s *Server) addClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[client.ID()] = client
	slog.Info("Client connected", "client", client.ID(), "clients", len(s.clients))
}

func (s *Server) removeClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, client.ID())
	slog.Info("Client disconnected", "client", client.ID(), "clients", len(s.clients))
}

// logPeerCredentials logs the UID/GID/PID of the connecting peer via
// SO_PEERCRED. The socket's group permission is the actual access-control
// boundary; this exists purely for attribution, so a connection from an
// unexpected user shows up in the helper's logs.
func logPeerCredentials(clientID string, conn net.Conn) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return
	}

	raw, err := unixConn.SyscallConn()
	if err != nil {
		slog.Debug("Failed to get raw connection for peer credentials", "client", clientID, "error", err)
		return
	}

	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		slog.Debug("Failed to read peer credentials", "client", clientID, "error", err)
		return
	}
	if credErr != nil {
		slog.Debug("Failed to read peer credentials", "client", clientID, "error", credErr)
		return
	}

	slog.Info("Peer credentials", "client", clientID, "uid", cred.Uid, "gid", cred.Gid, "pid", cred.Pid)
}

func (s *Server) handleClient(client *Client) {
	defer func() {
		// Release connection semaphore
		<-s.connSemaphore

		if err := client.Close(); err != nil {
			slog.Debug("Failed to close client connection", "error", err)
		}
		s.removeClient(client)
	}()

	// Use Scanner with size limit to prevent DoS via unbounded memory allocation
	scanner := bufio.NewScanner(client.conn)
	scanner.Buffer(make([]byte, initialBufferSize), maxMessageSize)

	for scanner.Scan() {
		line := scanner.Bytes()

		var req protocol.Request
		if err := json.Unmarshal(line, &req); err != nil {
			slog.Warn("Invalid request", "error", err)
			resp := protocol.NewErrorResponse("", protocol.ErrCodeInvalidRequest, "invalid JSON")
			if err := client.SendResponse(resp); err != nil {
				slog.Warn("Failed to send error response", "error", err)
			}
			continue
		}

		// Handle the request
		resp := s.handler(&req, client.ID())
		if err := client.SendResponse(resp); err != nil {
			slog.Error("Failed to send response", "error", err)
			return
		}
	}

	// Check for scanner errors (including message too large)
	if err := scanner.Err(); err != nil {
		if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
			if errors.Is(err, bufio.ErrTooLong) {
				slog.Warn("Client sent message exceeding size limit", "maxSize", maxMessageSize)
				resp := protocol.NewErrorResponse("", protocol.ErrCodeInvalidRequest, "message too large")
				if sendErr := client.SendResponse(resp); sendErr != nil {
					slog.Debug("Failed to send error response", "error", sendErr)
				}
			} else {
				slog.Error("Read error", "error", err)
			}
		}
	}
}

// Client represents a connected client.
type Client struct {
	id   string
	conn net.Conn
	mu   sync.Mutex
}

func newClient(id string, conn net.Conn) *Client {
	return &Client{
		id:   id,
		conn: conn,
	}
}

// ID returns the server-assigned identifier for this client connection.
func (c *Client) ID() string {
	return c.id
}

// SendResponse sends a response to the client.
func (c *Client) SendResponse(resp *protocol.Response) error {
	return c.sendJSON(resp)
}

// SendEvent sends an event to the client.
func (c *Client) SendEvent(event *protocol.Event) error {
	return c.sendJSON(event)
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) sendJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	_, err = c.conn.Write(data)
	return err
}
