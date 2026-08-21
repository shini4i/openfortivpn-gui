// Package main provides the entry point for openfortivpn-gui application.
// openfortivpn-gui is a GTK4/libadwaita GUI client for Fortinet SSL VPN on Linux,
// wrapping the openfortivpn CLI tool.
package main

import (
	"log/slog"
	"os"

	"github.com/shini4i/openfortivpn-gui/internal/logging"
	"github.com/shini4i/openfortivpn-gui/internal/ui"
)

func main() {
	// Initialize structured logging
	logging.SetupFromEnv()

	// No override: take the openfortivpn path from the stored configuration.
	app, err := ui.NewApp(&ui.AppConfig{
		OpenfortivpnPath: "",
	})
	if err != nil {
		slog.Error("Failed to initialize application", "error", err)
		os.Exit(1)
	}

	// Run the application
	if code := app.Run(os.Args); code > 0 {
		os.Exit(code)
	}
}
