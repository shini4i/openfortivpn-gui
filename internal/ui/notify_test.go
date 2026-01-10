package ui

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewNotifier(t *testing.T) {
	// NewNotifier with nil app should work (used in tests)
	notifier := NewNotifier(nil)
	assert.NotNil(t, notifier)
	assert.True(t, notifier.IsEnabled(), "notifier should be enabled by default")
}

func TestNotifier_SetEnabled(t *testing.T) {
	notifier := NewNotifier(nil)

	// Default is enabled
	assert.True(t, notifier.IsEnabled())

	// Disable
	notifier.SetEnabled(false)
	assert.False(t, notifier.IsEnabled())

	// Re-enable
	notifier.SetEnabled(true)
	assert.True(t, notifier.IsEnabled())
}

func TestNotifier_Notify_DisabledNotifier(t *testing.T) {
	notifier := NewNotifier(nil)
	notifier.SetEnabled(false)

	// Should not panic when disabled
	notifier.Notify(NotifyConnected, "TestProfile")
	notifier.Notify(NotifyDisconnected, "TestProfile")
	notifier.Notify(NotifyConnectionFailed, "TestProfile")
	notifier.Notify(NotifyReconnecting, "TestProfile")
}

func TestNotifier_Notify_NilApp(t *testing.T) {
	notifier := NewNotifier(nil)

	// Should not panic with nil app
	notifier.Notify(NotifyConnected, "TestProfile")
	notifier.Notify(NotifyDisconnected, "TestProfile")
	notifier.Notify(NotifyConnectionFailed, "TestProfile")
	notifier.Notify(NotifyReconnecting, "TestProfile")
}

func TestNotifier_Notify_InvalidType(t *testing.T) {
	notifier := NewNotifier(nil)

	// Invalid notification type should return early without panic
	notifier.Notify(NotificationType(999), "TestProfile")
}

func TestNotifier_ConvenienceMethods_NilApp(t *testing.T) {
	notifier := NewNotifier(nil)

	// All convenience methods should not panic with nil app
	notifier.NotifyConnected("TestProfile")
	notifier.NotifyDisconnected("TestProfile")
	notifier.NotifyConnectionFailed("TestProfile")
	notifier.NotifyReconnecting("TestProfile")
}

func TestNotifier_ConcurrentAccess(t *testing.T) {
	notifier := NewNotifier(nil)

	iterations := 1000
	if testing.Short() {
		iterations = 100
	}

	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				notifier.SetEnabled(j%2 == 0)
			}
		}()
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = notifier.IsEnabled()
			}
		}()
	}

	// Concurrent Notify calls (with nil app, just tests the enabled check)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				notifier.Notify(NotifyConnected, "TestProfile")
			}
		}()
	}

	wg.Wait()
}
