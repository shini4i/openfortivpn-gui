package ui

import "sync"

// connectionCounter identifies the connection a callback belongs to. VPN
// callbacks are raised on the controller's goroutine but do their work on the
// GTK main thread, so reading the count when a callback is raised and checking
// it again when it runs tells a superseded connection from the current one.
type connectionCounter struct {
	mu    sync.Mutex
	count uint64
}

// begin records a newly started connection and returns its count.
func (c *connectionCounter) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	return c.count
}

// current returns the count of the connection in flight.
func (c *connectionCounter) current() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// isCurrent reports whether count still identifies the connection in flight.
func (c *connectionCounter) isCurrent(count uint64) bool {
	return c.current() == count
}
