// Package server provides the UNIX socket server for the helper daemon.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/helper/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testHandler is a simple handler for testing.
func testHandler(req *protocol.Request) *protocol.Response {
	resp, err := protocol.NewSuccessResponse(req.ID, map[string]string{"status": "ok"})
	if err != nil {
		panic(fmt.Sprintf("testHandler: NewSuccessResponse failed: %v", err))
	}
	return resp
}

// waitForClientCount polls the server's ClientCount until it matches the expected value
// or the timeout elapses. It fails the test if the timeout is reached.
func waitForClientCount(t *testing.T, server *Server, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if server.ClientCount() == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitForClientCount: expected %d clients, got %d after %v", expected, server.ClientCount(), timeout)
}

// TestServerStartStop tests basic server lifecycle.
func TestServerStartStop(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	server := NewServerWithGroup(socketPath, "", testHandler)

	err := server.Start()
	require.NoError(t, err)

	// Verify server is running
	assert.Equal(t, 0, server.ClientCount())

	err = server.Stop()
	require.NoError(t, err)

	// Verify socket file is removed
	_, err = os.Stat(socketPath)
	assert.True(t, os.IsNotExist(err))
}

// TestServerDoubleStart tests that starting a running server returns an error.
func TestServerDoubleStart(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	server := NewServerWithGroup(socketPath, "", testHandler)

	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	err = server.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

// TestServerMaxMessageSize tests that messages exceeding the size limit are rejected.
func TestServerMaxMessageSize(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	server := NewServerWithGroup(socketPath, "", testHandler)

	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Connect to the server
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Wait for connection to be established
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, server.ClientCount())

	// Send a message that exceeds the limit (64KB + some extra)
	// We need to send without a newline to trigger the buffer overflow
	largeData := strings.Repeat("x", maxMessageSize+1000)

	// Write the oversized message without newline
	_, err = conn.Write([]byte(largeData))
	require.NoError(t, err)

	// The server should close the connection or send an error
	// Set a read timeout to avoid hanging
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	reader := bufio.NewReader(conn)
	response, err := reader.ReadBytes('\n')

	if err == nil {
		// Server sent an error response
		var errResp protocol.Response
		err := json.Unmarshal(response, &errResp)
		require.NoError(t, err)
		assert.NotNil(t, errResp.Error)
		assert.Contains(t, errResp.Error.Message, "message too large")
	}
	// If err != nil, server closed connection which is also acceptable
}

// TestServerValidRequest tests that valid requests are processed correctly.
func TestServerValidRequest(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	server := NewServerWithGroup(socketPath, "", testHandler)

	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Connect to the server
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send a valid request
	req := protocol.Request{
		ID:      "test-1",
		Command: "status",
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	_, err = conn.Write(append(data, '\n'))
	require.NoError(t, err)

	// Read response
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	reader := bufio.NewReader(conn)
	response, err := reader.ReadBytes('\n')
	require.NoError(t, err)

	var resp protocol.Response
	err = json.Unmarshal(response, &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-1", resp.ID)
	assert.Nil(t, resp.Error)
}

// TestServerMaxConcurrentClients tests that the connection limit is enforced.
func TestServerMaxConcurrentClients(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	server := NewServerWithGroup(socketPath, "", testHandler)

	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Create connections up to the limit
	conns := make([]net.Conn, 0, maxConcurrentClients)
	for i := 0; i < maxConcurrentClients; i++ {
		conn, err := net.Dial("unix", socketPath)
		require.NoError(t, err, "Failed to create connection %d", i)
		conns = append(conns, conn)
	}

	// Wait for all connections to be registered using polling
	waitForClientCount(t, server, maxConcurrentClients, 1*time.Second)

	// Try to create one more connection - should be rejected
	extraConn, err := net.Dial("unix", socketPath)
	if err == nil {
		// Connection was accepted at the OS level, but server should reject it
		// The extra connection should be closed by the server
		_ = extraConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 1)
		_, readErr := extraConn.Read(buf)
		// Expect EOF or closed connection error
		assert.Error(t, readErr, "Expected extra connection to be closed")
		_ = extraConn.Close()
	}

	// Close all connections
	for _, conn := range conns {
		_ = conn.Close()
	}

	// Wait for cleanup using polling
	waitForClientCount(t, server, 0, 1*time.Second)
}

// TestServerBroadcast tests that events are broadcast to all clients.
func TestServerBroadcast(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	server := NewServerWithGroup(socketPath, "", testHandler)

	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Create multiple client connections
	numClients := 3
	conns := make([]net.Conn, numClients)
	readers := make([]*bufio.Reader, numClients)

	for i := 0; i < numClients; i++ {
		conn, err := net.Dial("unix", socketPath)
		require.NoError(t, err)
		conns[i] = conn
		readers[i] = bufio.NewReader(conn)
	}
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()

	// Wait for connections to be established
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, numClients, server.ClientCount())

	// Broadcast an event
	event, err := protocol.NewEvent(protocol.EventOutput, protocol.OutputData{Line: "test broadcast"})
	require.NoError(t, err)
	server.Broadcast(event)

	// All clients should receive the event
	var wg sync.WaitGroup
	received := make([]bool, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = conns[idx].SetReadDeadline(time.Now().Add(2 * time.Second))
			data, err := readers[idx].ReadBytes('\n')
			if err == nil {
				var evt protocol.Event
				if json.Unmarshal(data, &evt) == nil {
					received[idx] = true
				}
			}
		}(i)
	}

	wg.Wait()

	for i, r := range received {
		assert.True(t, r, "Client %d did not receive broadcast", i)
	}
}

// TestNewServerWithGroupNilHandler tests that nil handler causes panic.
func TestNewServerWithGroupNilHandler(t *testing.T) {
	assert.Panics(t, func() {
		NewServerWithGroup("/tmp/test.sock", "", nil)
	})
}

// TestServerInvalidJSON tests that invalid JSON requests are handled gracefully.
func TestServerInvalidJSON(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	server := NewServerWithGroup(socketPath, "", testHandler)

	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send invalid JSON
	_, err = conn.Write([]byte("not valid json\n"))
	require.NoError(t, err)

	// Read error response
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	reader := bufio.NewReader(conn)
	response, err := reader.ReadBytes('\n')
	require.NoError(t, err)

	var resp protocol.Response
	err = json.Unmarshal(response, &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, protocol.ErrCodeInvalidRequest, resp.Error.Code)
}
