package vpn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// MockProcess implements Process for testing.
type MockProcess struct {
	mu sync.Mutex

	startErr error
	waitErr  error
	killErr  error

	stdin  *mockWriteCloser
	stdout *mockReadCloser
	stderr *mockReadCloser

	started bool
	killed  bool
	waited  bool

	// WaitCh can be used to control when Wait() returns
	WaitCh chan struct{}
}

// NewMockProcess creates a new mock process with default buffers.
func NewMockProcess() *MockProcess {
	return &MockProcess{
		stdin:  &mockWriteCloser{buf: &bytes.Buffer{}},
		stdout: &mockReadCloser{buf: &bytes.Buffer{}},
		stderr: &mockReadCloser{buf: &bytes.Buffer{}},
		WaitCh: make(chan struct{}),
	}
}

func (p *MockProcess) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.startErr != nil {
		return p.startErr
	}
	p.started = true
	return nil
}

func (p *MockProcess) Wait() error {
	// Wait for signal to complete
	<-p.WaitCh
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waited = true
	return p.waitErr
}

func (p *MockProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killErr != nil {
		return p.killErr
	}
	p.killed = true
	// Close the wait channel to unblock Wait()
	select {
	case <-p.WaitCh:
		// Already closed
	default:
		close(p.WaitCh)
	}
	return nil
}

func (p *MockProcess) Stdin() io.WriteCloser {
	return p.stdin
}

func (p *MockProcess) Stdout() io.ReadCloser {
	return p.stdout
}

func (p *MockProcess) Stderr() io.ReadCloser {
	return p.stderr
}

// SetStartError sets an error to return from Start().
func (p *MockProcess) SetStartError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.startErr = err
}

// SetWaitError sets an error to return from Wait().
func (p *MockProcess) SetWaitError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waitErr = err
}

// SetKillError sets an error to return from Kill().
func (p *MockProcess) SetKillError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killErr = err
}

// WriteToStdout writes data to stdout for the controller to read.
func (p *MockProcess) WriteToStdout(data string) {
	p.stdout.buf.WriteString(data + "\n")
}

// WriteToStderr writes data to stderr for the controller to read.
func (p *MockProcess) WriteToStderr(data string) {
	p.stderr.buf.WriteString(data + "\n")
}

// GetStdinContent returns what was written to stdin.
func (p *MockProcess) GetStdinContent() string {
	p.stdin.mu.Lock()
	defer p.stdin.mu.Unlock()
	return p.stdin.buf.String()
}

// IsStarted returns true if Start() was called.
func (p *MockProcess) IsStarted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.started
}

// IsKilled returns true if Kill() was called.
func (p *MockProcess) IsKilled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// CompleteProcess signals that the process should complete.
func (p *MockProcess) CompleteProcess() {
	select {
	case <-p.WaitCh:
		// Already closed
	default:
		close(p.WaitCh)
	}
}

// mockWriteCloser implements io.WriteCloser for testing.
type mockWriteCloser struct {
	buf    *bytes.Buffer
	closed bool
	mu     sync.Mutex
}

func (w *mockWriteCloser) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, errors.New("write to closed writer")
	}
	return w.buf.Write(p)
}

func (w *mockWriteCloser) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

// mockReadCloser implements io.ReadCloser for testing.
type mockReadCloser struct {
	buf    *bytes.Buffer
	closed bool
	mu     sync.Mutex
}

func (r *mockReadCloser) Read(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.EOF
	}
	return r.buf.Read(p)
}

func (r *mockReadCloser) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// MockExecutor implements ProcessExecutor for testing.
type MockExecutor struct {
	mu sync.Mutex

	createErr error
	process   *MockProcess

	// Captured values
	lastCtx  context.Context
	lastName string
	lastArgs []string
}

// NewMockExecutor creates a new mock executor.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		process: NewMockProcess(),
	}
}

// CreateProcess implements ProcessExecutor.
func (e *MockExecutor) CreateProcess(ctx context.Context, name string, args ...string) (Process, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.lastCtx = ctx
	e.lastName = name
	e.lastArgs = args

	if e.createErr != nil {
		return nil, e.createErr
	}

	return e.process, nil
}

// SetCreateError sets an error to return from CreateProcess.
func (e *MockExecutor) SetCreateError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.createErr = err
}

// GetProcess returns the mock process.
func (e *MockExecutor) GetProcess() *MockProcess {
	return e.process
}

// GetLastName returns the last command name passed to CreateProcess.
func (e *MockExecutor) GetLastName() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastName
}

// GetLastArgs returns the last args passed to CreateProcess.
func (e *MockExecutor) GetLastArgs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastArgs
}
