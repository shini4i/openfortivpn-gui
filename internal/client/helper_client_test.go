package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/openfortivpn-gui/internal/helper/protocol"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// fakeDaemon is a minimal stand-in for the helper daemon. It listens on a unix
// socket, answers the status handshake that NewHelperClientWithPath performs,
// and captures the parameters of the connect request so tests can assert what
// the client actually transmitted over the wire.
type fakeDaemon struct {
	path        string
	connectArgs chan protocol.ConnectParams

	// state and assignedIP are what the status handshake reports, letting a
	// test start its client from an already-connected daemon.
	state      vpn.ConnectionState
	assignedIP string

	mu   sync.Mutex
	conn net.Conn
	held chan struct{}

	// heldReqs carries the commands read while responses are held.
	heldReqs chan protocol.Command
}

// daemonSeq keeps each fake daemon on its own abstract socket.
var daemonSeq atomic.Uint64

// startFakeDaemon launches a fake helper daemon on an abstract unix socket (the
// "@" prefix keeps it clear of the kernel's sun_path limit, which a long CI
// $TMPDIR can overrun). Its status handshake reports state and assignedIP, and
// the listener closes with the test.
func startFakeDaemon(t *testing.T, state vpn.ConnectionState, assignedIP string) *fakeDaemon {
	t.Helper()

	path := fmt.Sprintf("@openfortivpn-gui-test-%d-%d.sock", os.Getpid(), daemonSeq.Add(1))
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	d := &fakeDaemon{
		path:        path,
		connectArgs: make(chan protocol.ConnectParams, 1),
		state:       state,
		assignedIP:  assignedIP,
		heldReqs:    make(chan protocol.Command, 4),
	}

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		d.mu.Lock()
		d.conn = conn
		d.mu.Unlock()

		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}

			var req protocol.Request
			if err := json.Unmarshal(line, &req); err != nil {
				continue
			}

			d.mu.Lock()
			held := d.held
			d.mu.Unlock()

			// A held request is read but never answered, so the client is left
			// waiting on the wire until something else frees it.
			if held != nil {
				d.heldReqs <- req.Command
				<-held
				return
			}

			var resp *protocol.Response
			switch req.Command {
			case protocol.CommandStatus:
				resp, _ = protocol.NewSuccessResponse(req.ID, protocol.StatusResult{
					State:      string(d.state),
					AssignedIP: d.assignedIP,
				})
			case protocol.CommandConnect:
				var params protocol.ConnectParams
				_ = json.Unmarshal(req.Params, &params)
				d.connectArgs <- params
				resp, _ = protocol.NewSuccessResponse(req.ID, nil)
			default:
				resp, _ = protocol.NewSuccessResponse(req.ID, nil)
			}

			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			if _, err := conn.Write(data); err != nil {
				return
			}
		}
	}()

	return d
}

// dropConnection closes the accepted connection, standing in for a helper
// daemon that crashes or is killed while a client is still using it.
func (d *fakeDaemon) dropConnection(t *testing.T) {
	t.Helper()

	d.mu.Lock()
	defer d.mu.Unlock()
	require.NotNil(t, d.conn, "daemon has not accepted a connection yet")
	require.NoError(t, d.conn.Close())
	if d.held != nil {
		close(d.held)
	}
}

// holdResponses stops the daemon answering from the next request on, leaving it
// in flight. Call it after the client's status handshake has completed.
func (d *fakeDaemon) holdResponses() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.held = make(chan struct{})
}

// TestHelperClient_Connect_ForwardsPasswordAndOTP verifies the GUI->daemon hop:
// a 2FA connection transmits both the account password and the OTP (along with
// the rest of the profile) to the helper daemon over the socket. The in-process
// controller tests cannot exercise this boundary, so this guards the wire that
// carries credentials from the GUI to the privileged daemon.
func TestHelperClient_Connect_ForwardsPasswordAndOTP(t *testing.T) {
	daemon := startFakeDaemon(t, vpn.StateDisconnected, "")

	client, err := NewHelperClientWithPath(daemon.path)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodOTP,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err = client.Connect(context.Background(), p, &vpn.ConnectOptions{
		Password: "topsecret",
		OTP:      "123456",
	})
	require.NoError(t, err)

	select {
	case params := <-daemon.connectArgs:
		assert.Equal(t, "topsecret", params.Password, "password must be forwarded to the daemon")
		assert.Equal(t, "123456", params.OTP, "OTP must be forwarded to the daemon")
		assert.Equal(t, "otp", params.AuthMethod, "auth method must be forwarded")
		assert.Equal(t, "testuser", params.Username)
		assert.Equal(t, "vpn.example.com", params.Host)
		assert.Equal(t, 443, params.Port)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not receive a connect request")
	}
}

func TestHelperClient_StoreInterfaceIfCurrent(t *testing.T) {
	newConnected := func() *HelperClient {
		c := &HelperClient{state: vpn.StateConnected, assignedIP: "10.0.0.2"}
		return c
	}

	t.Run("stores the interface for the current address", func(t *testing.T) {
		c := newConnected()

		assert.True(t, c.storeInterfaceIfCurrent("10.0.0.2", "ppp0"))
		assert.Equal(t, "ppp0", c.GetInterface())
	})

	t.Run("rejects an interface for a stale address", func(t *testing.T) {
		c := newConnected()

		assert.False(t, c.storeInterfaceIfCurrent("10.0.0.9", "ppp0"))
		assert.Empty(t, c.GetInterface())
	})

	t.Run("rejects an interface once disconnected", func(t *testing.T) {
		c := newConnected()
		c.state = vpn.StateDisconnected

		assert.False(t, c.storeInterfaceIfCurrent("10.0.0.2", "ppp0"))
		assert.Empty(t, c.GetInterface())
	})
}

// stateChangeEvent builds the daemon's state-change event for handleEvent.
func stateChangeEvent(t *testing.T, from, to vpn.ConnectionState) *protocol.Event {
	t.Helper()

	data, err := json.Marshal(protocol.StateChangeData{From: string(from), To: string(to)})
	require.NoError(t, err)
	return &protocol.Event{
		Type: protocol.MessageTypeEvent,
		Name: protocol.EventStateChange,
		Data: data,
	}
}

// TestHelperClient_HandleEvent_ClearsAddressingOnTerminalState holds the
// interface.go contract on the helper path: a tunnel reports no address once it
// is gone, and a failed tunnel is as gone as a disconnected one.
func TestHelperClient_HandleEvent_ClearsAddressingOnTerminalState(t *testing.T) {
	for _, terminal := range []vpn.ConnectionState{vpn.StateDisconnected, vpn.StateFailed} {
		t.Run(string(terminal), func(t *testing.T) {
			c := &HelperClient{
				state:         vpn.StateConnected,
				assignedIP:    "10.0.0.2",
				interfaceName: "ppp0",
			}

			c.handleEvent(stateChangeEvent(t, vpn.StateConnected, terminal))

			assert.Equal(t, terminal, c.GetState())
			assert.Empty(t, c.GetAssignedIP())
			assert.Empty(t, c.GetInterface())
		})
	}
}

// TestHelperClient_HelperDeath_ReportsTerminalState covers the helper daemon
// dying under a live tunnel. The socket closes with no disconnect event, so the
// read loop is the only thing that can notice; without a terminal report the
// GUI shows "Connected" for a tunnel that is gone, and never re-checks.
func TestHelperClient_HelperDeath_ReportsTerminalState(t *testing.T) {
	daemon := startFakeDaemon(t, vpn.StateConnected, "10.0.0.2")

	client, err := NewHelperClientWithPath(daemon.path)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	require.Equal(t, vpn.StateConnected, client.GetState())

	states := make(chan [2]vpn.ConnectionState, 4)
	errs := make(chan error, 4)
	client.OnStateChange(func(old, next vpn.ConnectionState) {
		states <- [2]vpn.ConnectionState{old, next}
	})
	client.OnError(func(err error) { errs <- err })

	daemon.dropConnection(t)

	select {
	case got := <-states:
		assert.Equal(t, vpn.StateConnected, got[0])
		assert.Equal(t, vpn.StateFailed, got[1])
	case <-time.After(2 * time.Second):
		t.Fatal("a dead helper produced no state change")
	}

	select {
	case err := <-errs:
		assert.ErrorIs(t, err, ErrHelperConnectionLost)
	case <-time.After(2 * time.Second):
		t.Fatal("a dead helper produced no error report")
	}

	assert.Equal(t, vpn.StateFailed, client.GetState())
	assert.Empty(t, client.GetAssignedIP(), "a dead helper's addressing must not survive")
	assert.Empty(t, client.GetInterface())
}

// TestHelperClient_HelperDeath_WhileIdle_ReportsErrorOnly covers a helper that
// dies with no tunnel up — a daemon restart, typically. Nothing failed, so the
// GUI must not be told a connection did.
func TestHelperClient_HelperDeath_WhileIdle_ReportsErrorOnly(t *testing.T) {
	for _, quiet := range []vpn.ConnectionState{vpn.StateDisconnected, vpn.StateFailed} {
		t.Run(string(quiet), func(t *testing.T) {
			daemon := startFakeDaemon(t, quiet, "")

			client, err := NewHelperClientWithPath(daemon.path)
			require.NoError(t, err)
			defer func() { _ = client.Close() }()

			states := make(chan [2]vpn.ConnectionState, 4)
			errs := make(chan error, 4)
			client.OnStateChange(func(old, next vpn.ConnectionState) {
				states <- [2]vpn.ConnectionState{old, next}
			})
			client.OnError(func(err error) { errs <- err })

			daemon.dropConnection(t)

			select {
			case err := <-errs:
				assert.ErrorIs(t, err, ErrHelperConnectionLost)
			case <-time.After(2 * time.Second):
				t.Fatal("a dead helper produced no error report")
			}

			assert.Empty(t, states, "an idle helper death must not report a failed connection")
		})
	}
}

// TestHelperClient_HelperDeath_UnblocksInFlightRequest covers the request that
// was already on the wire when the helper died. Nothing will ever answer it, so
// losing the helper has to free it rather than leave it to time out.
func TestHelperClient_HelperDeath_UnblocksInFlightRequest(t *testing.T) {
	daemon := startFakeDaemon(t, vpn.StateConnected, "10.0.0.2")

	client, err := NewHelperClientWithPath(daemon.path)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	daemon.holdResponses()

	done := make(chan error, 1)
	go func() { done <- client.Disconnect(context.Background()) }()

	select {
	case cmd := <-daemon.heldReqs:
		require.Equal(t, protocol.CommandDisconnect, cmd)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon never received the disconnect request")
	}

	daemon.dropConnection(t)

	select {
	case err := <-done:
		require.Error(t, err)
		assert.NotErrorIs(t, err, context.DeadlineExceeded,
			"the request must be freed by the lost helper, not by its own timeout")
	case <-time.After(2 * time.Second):
		t.Fatal("a dead helper left an in-flight request blocked")
	}
}

// TestHelperClient_Close_ReportsNothing guards the other side of the same
// branch: a shutdown the GUI asked for is not a lost helper, and must not
// raise an error dialog on the way out.
func TestHelperClient_Close_ReportsNothing(t *testing.T) {
	daemon := startFakeDaemon(t, vpn.StateConnected, "10.0.0.2")

	client, err := NewHelperClientWithPath(daemon.path)
	require.NoError(t, err)

	errs := make(chan error, 4)
	client.OnError(func(err error) { errs <- err })

	require.NoError(t, client.Close())

	select {
	case err := <-errs:
		t.Fatalf("a deliberate close reported an error: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	assert.Equal(t, vpn.StateConnected, client.GetState(),
		"a deliberate close must not mark the tunnel failed")
}
