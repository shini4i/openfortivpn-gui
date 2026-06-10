// Package main provides the entry point for the openfortivpn-gui-helper daemon.
//
// The helper daemon runs as a systemd service with root privileges and handles
// VPN connection management on behalf of unprivileged GUI clients. Communication
// happens over a UNIX socket using JSON messages.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/helper/manager"
	"github.com/shini4i/openfortivpn-gui/internal/helper/protocol"
	"github.com/shini4i/openfortivpn-gui/internal/helper/server"
)

const (
	defaultOpenfortivpnPath = "/usr/bin/openfortivpn"
)

var (
	version = "dev"
)

func main() {
	// Parse command line flags
	socketPath := flag.String("socket", server.DefaultSocketPath, "Path to the UNIX socket")
	openfortivpnPath := flag.String("openfortivpn", defaultOpenfortivpnPath, "Path to openfortivpn binary")
	showVersion := flag.Bool("version", false, "Show version and exit")
	debug := flag.Bool("debug", os.Getenv("OPENFORTIVPN_GUI_DEBUG") == "1",
		"Enable debug logging (also enabled via OPENFORTIVPN_GUI_DEBUG=1)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("openfortivpn-gui-helper %s\n", version)
		os.Exit(0)
	}

	// Configure structured logging
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	slog.Info("Starting openfortivpn-gui-helper", "version", version)

	// Verify openfortivpn binary exists
	if _, err := exec.LookPath(*openfortivpnPath); err != nil {
		slog.Error("openfortivpn binary not found", "path", *openfortivpnPath, "error", err)
		os.Exit(1)
	}

	// Create thread-safe event sender to avoid race condition during initialization
	sender := &safeEventSender{}

	// Create manager and server
	mgr := manager.NewManager(*openfortivpnPath, sender)
	srv := server.NewServer(*socketPath, mgr.HandleRequest)

	// Now that server is created, set it in the sender
	sender.SetServer(srv)

	// Start server
	if err := srv.Start(); err != nil {
		slog.Error("Failed to start server", "error", err)
		os.Exit(1)
	}

	// Notify systemd that we're ready
	notifySystemd("READY=1")

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start watchdog goroutine if enabled
	go watchdogLoop()

	// Wait for shutdown signal
	sig := <-sigChan
	slog.Info("Received shutdown signal", "signal", sig)

	// Notify systemd we're stopping
	notifySystemd("STOPPING=1")

	// Graceful shutdown with timeout to prevent hanging indefinitely
	const shutdownTimeout = 10 * time.Second
	shutdownDone := make(chan struct{})

	go func() {
		mgr.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		slog.Info("Manager shutdown completed")
	case <-time.After(shutdownTimeout):
		slog.Warn("Manager shutdown timed out", "timeout", shutdownTimeout)
	}

	// Always stop the server, even if manager shutdown timed out
	if err := srv.Stop(); err != nil {
		slog.Error("Error stopping server", "error", err)
	}

	slog.Info("Shutdown complete")
}

// notifySystemd sends a notification to systemd.
func notifySystemd(state string) {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return
	}

	// Handle abstract sockets (prefixed with @) by replacing @ with null byte
	if socketPath[0] == '@' {
		socketPath = "\x00" + socketPath[1:]
	}

	// #nosec G704 -- not SSRF: "unixgram" dials a local Unix datagram socket, not a network endpoint, and socketPath is NOTIFY_SOCKET supplied by systemd (the trusted supervisor that launched this helper)
	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		slog.Warn("Failed to connect to notify socket", "error", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Debug("Failed to close notify socket", "error", err)
		}
	}()

	if _, err := conn.Write([]byte(state)); err != nil {
		slog.Warn("Failed to notify systemd", "error", err)
	}
}

// watchdogLoop sends periodic watchdog notifications to systemd.
func watchdogLoop() {
	// Check if watchdog is enabled
	watchdogUsec := os.Getenv("WATCHDOG_USEC")
	if watchdogUsec == "" {
		return
	}

	// Parse interval (in microseconds)
	var usec int64
	if _, err := fmt.Sscanf(watchdogUsec, "%d", &usec); err != nil {
		slog.Warn("Invalid WATCHDOG_USEC", "value", watchdogUsec)
		return
	}

	// Notify at half the watchdog interval
	interval := time.Duration(usec/2) * time.Microsecond

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		notifySystemd("WATCHDOG=1")
	}
}

// safeEventSender provides thread-safe event delivery to clients.
// This avoids a race condition during initialization where the server
// might not be set yet when events are sent. It implements manager.EventSender.
type safeEventSender struct {
	mu  sync.RWMutex
	srv *server.Server
}

// SetServer sets the server used for event delivery.
func (s *safeEventSender) SetServer(srv *server.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.srv = srv
}

// Broadcast sends an event to all connected clients.
func (s *safeEventSender) Broadcast(event *protocol.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.srv != nil {
		s.srv.Broadcast(event)
	}
}

// SendToClient sends an event to a single client.
func (s *safeEventSender) SendToClient(clientID string, event *protocol.Event) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.srv != nil {
		return s.srv.SendToClient(clientID, event)
	}
	return nil
}
