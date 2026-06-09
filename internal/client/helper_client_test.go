package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
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
}

// startFakeDaemon launches a fake helper daemon on a unix socket and returns
// it. The listener is closed automatically when the test finishes.
//
// It binds to a Linux abstract-namespace socket (the "@" prefix) rather than a
// filesystem path: t.TempDir() under a long $TMPDIR (e.g. CI runners) can push
// the socket path past the kernel's ~108-byte sun_path limit, which fails with
// "bind: invalid argument". Abstract sockets carry no filesystem path and need
// no cleanup. The transport (NDJSON over a unix conn) is identical either way.
func startFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()

	path := fmt.Sprintf("@openfortivpn-gui-test-%d.sock", os.Getpid())
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	d := &fakeDaemon{
		path:        path,
		connectArgs: make(chan protocol.ConnectParams, 1),
	}

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

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

			var resp *protocol.Response
			switch req.Command {
			case protocol.CommandStatus:
				resp, _ = protocol.NewSuccessResponse(req.ID, protocol.StatusResult{
					State: string(vpn.StateDisconnected),
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

// TestHelperClient_Connect_ForwardsPasswordAndOTP verifies the GUI->daemon hop:
// a 2FA connection transmits both the account password and the OTP (along with
// the rest of the profile) to the helper daemon over the socket. The in-process
// controller tests cannot exercise this boundary, so this guards the wire that
// carries credentials from the GUI to the privileged daemon.
func TestHelperClient_Connect_ForwardsPasswordAndOTP(t *testing.T) {
	daemon := startFakeDaemon(t)

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
