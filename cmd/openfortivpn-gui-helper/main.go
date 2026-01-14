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
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

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
	flag.Parse()

	if *showVersion {
		fmt.Printf("openfortivpn-gui-helper %s\n", version)
		os.Exit(0)
	}

	// Configure structured logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("Starting openfortivpn-gui-helper", "version", version)

	// Verify openfortivpn binary exists
	if _, err := exec.LookPath(*openfortivpnPath); err != nil {
		slog.Error("openfortivpn binary not found", "path", *openfortivpnPath, "error", err)
		os.Exit(1)
	}

	// Create thread-safe broadcaster to avoid race condition during initialization
	broadcaster := &safeBroadcaster{}

	// Create manager and server
	mgr := manager.NewManager(*openfortivpnPath, broadcaster.Broadcast)
	srv := server.NewServer(*socketPath, mgr.HandleRequest)

	// Now that server is created, set it in the broadcaster
	broadcaster.SetServer(srv)

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

	// Graceful shutdown
	mgr.Shutdown()
	srv.Stop()

	slog.Info("Shutdown complete")
}

// notifySystemd sends a notification to systemd.
func notifySystemd(state string) {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return
	}

	conn, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		slog.Warn("Failed to create notify socket", "error", err)
		return
	}
	defer syscall.Close(conn)

	addr := &syscall.SockaddrUnix{Name: socketPath}
	if err := syscall.Sendto(conn, []byte(state), 0, addr); err != nil {
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
	interval := usec / 2

	for {
		// Sleep for half the watchdog interval (convert from microseconds)
		syscall.Select(0, nil, nil, nil, &syscall.Timeval{
			Sec:  interval / 1000000,
			Usec: interval % 1000000,
		})
		notifySystemd("WATCHDOG=1")
	}
}

// safeBroadcaster provides thread-safe event broadcasting to clients.
// This avoids a race condition during initialization where the server
// might not be set yet when events are broadcast.
type safeBroadcaster struct {
	mu  sync.RWMutex
	srv *server.Server
}

// SetServer sets the server for broadcasting.
func (b *safeBroadcaster) SetServer(srv *server.Server) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.srv = srv
}

// Broadcast sends an event to all connected clients.
func (b *safeBroadcaster) Broadcast(event *protocol.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.srv != nil {
		b.srv.Broadcast(event)
	}
}
