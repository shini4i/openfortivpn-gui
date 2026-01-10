package logging

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetup_Info(t *testing.T) {
	// Should not panic
	Setup(LevelInfo)
}

func TestSetup_Debug(t *testing.T) {
	// Should not panic
	Setup(LevelDebug)
}

func TestSetupFromEnv_Default(t *testing.T) {
	// Save and restore environment
	original := os.Getenv("OPENFORTIVPN_GUI_DEBUG")
	defer os.Setenv("OPENFORTIVPN_GUI_DEBUG", original)

	os.Unsetenv("OPENFORTIVPN_GUI_DEBUG")
	SetupFromEnv() // Should not panic, uses LevelInfo by default
}

func TestSetupFromEnv_Debug(t *testing.T) {
	// Save and restore environment
	original := os.Getenv("OPENFORTIVPN_GUI_DEBUG")
	defer os.Setenv("OPENFORTIVPN_GUI_DEBUG", original)

	os.Setenv("OPENFORTIVPN_GUI_DEBUG", "1")
	SetupFromEnv() // Should not panic, uses LevelDebug
}

func TestLevel_Values(t *testing.T) {
	assert.Equal(t, Level(0), LevelInfo)
	assert.Equal(t, Level(1), LevelDebug)
}
