package vpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
)

// ConnectOptions contains optional parameters for VPN connection.
type ConnectOptions struct {
	// Password for authentication (used with password and OTP auth methods).
	Password string
	// OTP is the one-time password for two-factor authentication.
	// When provided, it's passed to openfortivpn via the --otp flag.
	OTP string
}

// defaultOutputDrainTimeout bounds the wait for the output scanners once the
// process has exited. A grandchild that inherited the pipes keeps them open
// after the process itself is gone; the bound stops that from wedging the state
// machine.
const defaultOutputDrainTimeout = 5 * time.Second

// Controller manages VPN connection lifecycle using openfortivpn.
type Controller struct {
	openfortivpnPath string
	executor         ProcessExecutor
	directMode       bool // When true, run openfortivpn directly without pkexec

	// Read without mu from the completion goroutine, so it must never be
	// written after the controller's first Connect. Tests shorten it before.
	outputDrainTimeout time.Duration

	// Held for reading while an attempt dispatches callbacks, and for writing
	// while a new attempt starts, so a superseded attempt can never deliver a
	// callback to a newer connection. Always taken before mu, and never held by
	// a callback that calls Connect — that would deadlock against beginAttempt.
	dispatch sync.RWMutex

	mu            sync.RWMutex
	state         ConnectionState
	assignedIP    string
	interfaceName string

	// Identifies the current connection attempt. Stamped when the state moves
	// to Connecting, so a completion goroutine can tell whether the controller
	// has moved on without depending on when its process was registered.
	attempt uint64

	// Process management
	process Process
	ctx     context.Context
	cancel  context.CancelFunc
	stdin   io.WriteCloser

	// Callbacks
	onStateChange func(old, new ConnectionState)
	onOutput      func(line string)
	onEvent       func(event *OutputEvent)
	onError       func(err error)
}

// ControllerOption configures a Controller.
type ControllerOption func(*Controller)

// WithExecutor sets a custom process executor (primarily for testing).
func WithExecutor(executor ProcessExecutor) ControllerOption {
	return func(c *Controller) {
		c.executor = executor
	}
}

// WithDirectMode configures the controller for direct execution without pkexec.
// This is intended for the helper daemon which already runs with root privileges.
func WithDirectMode() ControllerOption {
	return func(c *Controller) {
		c.directMode = true
		c.executor = NewDirectExecutor()
	}
}

// NewController creates a new VPN controller instance.
// By default, it uses RealExecutor with pkexec for privilege escalation.
// Use WithExecutor or WithDirectMode options to customize behavior.
func NewController(openfortivpnPath string, opts ...ControllerOption) *Controller {
	c := &Controller{
		openfortivpnPath:   openfortivpnPath,
		executor:           NewRealExecutor(),
		state:              StateDisconnected,
		outputDrainTimeout: defaultOutputDrainTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetState returns the current connection state.
func (c *Controller) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// setState transitions to a new state if the transition is valid.
// The state change callback is invoked outside the lock to prevent deadlocks.
func (c *Controller) setState(newState ConnectionState) error {
	return c.transition(newState, nil)
}

// transition applies newState when the state machine allows it and allowed
// accepts the current state. allowed runs with the lock held and must not lock;
// nil accepts any state. Rejection by allowed is not an error.
func (c *Controller) transition(newState ConnectionState, allowed func(current ConnectionState) bool) error {
	c.mu.Lock()
	if allowed != nil && !allowed(c.state) {
		c.mu.Unlock()
		return nil
	}

	if !IsValidTransition(c.state, newState) {
		oldState := c.state
		c.mu.Unlock()
		return fmt.Errorf("invalid state transition from %s to %s", oldState, newState)
	}

	oldState := c.state
	c.state = newState
	callback := c.onStateChange
	c.mu.Unlock()

	// Call callback outside of lock to prevent deadlocks
	if callback != nil {
		callback(oldState, newState)
	}

	return nil
}

// beginAttempt transitions to Connecting and stamps the new attempt in the same
// critical section, so no completion goroutine can mistake the attempt starting
// here for its own. It first waits for any in-flight dispatch of the previous
// attempt to finish. Returns the attempt's identifier.
func (c *Controller) beginAttempt() (uint64, error) {
	c.dispatch.Lock()
	defer c.dispatch.Unlock()

	c.mu.Lock()
	if !IsValidTransition(c.state, StateConnecting) {
		oldState := c.state
		c.mu.Unlock()
		return 0, fmt.Errorf("invalid state transition from %s to %s", oldState, StateConnecting)
	}

	oldState := c.state
	c.state = StateConnecting
	c.attempt++
	attempt := c.attempt
	callback := c.onStateChange
	c.mu.Unlock()

	// Called outside mu to prevent deadlocks, but still under dispatch: a
	// consumer may use this announcement to tell one connection's callbacks
	// from the next one's, which only holds while the previous attempt's
	// dispatch is held off.
	if callback != nil {
		callback(oldState, StateConnecting)
	}

	return attempt, nil
}

// OnStateChange registers a callback for state changes.
func (c *Controller) OnStateChange(callback func(old, new ConnectionState)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onStateChange = callback
}

// OnOutput registers a callback for raw output lines.
func (c *Controller) OnOutput(callback func(line string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onOutput = callback
}

// OnEvent registers a callback for parsed events.
func (c *Controller) OnEvent(callback func(event *OutputEvent)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvent = callback
}

// OnError registers a callback for errors.
func (c *Controller) OnError(callback func(err error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onError = callback
}

// CanConnect returns true if a new connection can be initiated.
func (c *Controller) CanConnect() bool {
	return c.GetState().CanConnect()
}

// CanDisconnect returns true if the connection can be terminated.
func (c *Controller) CanDisconnect() bool {
	return c.GetState().CanDisconnect()
}

// isCurrentAttempt reports whether attempt is the one the controller is on.
func (c *Controller) isCurrentAttempt(attempt uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.attempt == attempt
}

// GetAssignedIP returns the IP address assigned by the VPN.
func (c *Controller) GetAssignedIP() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.assignedIP
}

// setAssignedIP sets the assigned IP address.
func (c *Controller) setAssignedIP(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.assignedIP = ip
}

// setAddressingForAttempt records the tunnel's address and interface, ignoring
// an attempt the controller has already moved past so its output cannot
// overwrite a newer connection's addressing. Reports whether it applied.
func (c *Controller) setAddressingForAttempt(attempt uint64, ip, iface string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.attempt != attempt {
		return false
	}
	c.assignedIP = ip
	c.interfaceName = iface
	return true
}

// GetInterface returns the network interface name used by the VPN tunnel.
func (c *Controller) GetInterface() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.interfaceName
}

// detectInterface attempts to detect the VPN interface by the assigned IP.
// It uses DetectInterfaceWithRetry for retry logic, then verifies the attempt is
// still current and the connection state still valid before setting the name.
func (c *Controller) detectInterface(attempt uint64, assignedIP string) {
	ifaceName, err := DetectInterfaceWithRetry(assignedIP, 5, 100*time.Millisecond, nil)
	if err != nil {
		slog.Warn("Failed to detect VPN interface after retries", "ip", assignedIP)
		return
	}

	// Verify state before setting interface to avoid race with newer connections.
	c.mu.Lock()
	currentIP := c.assignedIP
	currentState := c.state
	if c.attempt == attempt && currentIP == assignedIP && currentState != StateDisconnected {
		c.interfaceName = ifaceName
		c.mu.Unlock()
		slog.Info("Detected VPN interface", "interface", ifaceName, "ip", assignedIP)
	} else {
		c.mu.Unlock()
		slog.Debug("Skipping interface update: state changed during detection",
			"expectedIP", assignedIP, "currentIP", currentIP, "state", currentState)
	}
}

// emitOutput sends a raw output line to the registered callback.
func (c *Controller) emitOutput(line string) {
	c.mu.RLock()
	callback := c.onOutput
	c.mu.RUnlock()

	if callback != nil {
		callback(line)
	}
}

// emitEvent sends a parsed event to the registered callback.
func (c *Controller) emitEvent(event *OutputEvent) {
	c.mu.RLock()
	callback := c.onEvent
	c.mu.RUnlock()

	if callback != nil {
		callback(event)
	}
}

// emitError sends an error to the registered callback.
func (c *Controller) emitError(err error) {
	c.mu.RLock()
	callback := c.onError
	c.mu.RUnlock()

	if callback != nil {
		callback(err)
	}
}

// processOutput processes a single line of openfortivpn output on behalf of the
// given attempt. A superseded attempt's line still reaches the connection log,
// but nothing acts on it: its events would drive a newer connection's state,
// dialogs and stored credentials on behalf of a process that is already gone.
func (c *Controller) processOutput(attempt uint64, line string) {
	// Emit raw output
	c.emitOutput(line)

	// Parse the line
	event := ParseLine(line)
	if event == nil {
		return
	}

	// Held across the dispatch: a retry starting midway through would otherwise
	// receive this attempt's callbacks.
	c.dispatch.RLock()
	defer c.dispatch.RUnlock()

	if !c.isCurrentAttempt(attempt) {
		slog.Debug("Ignoring event from a superseded connection attempt",
			"event", event.Type, "attempt", attempt)
		return
	}

	// Emit parsed event
	c.emitEvent(event)

	// Runs under the state lock, so it reads c.attempt without taking it.
	isCurrent := func(ConnectionState) bool { return c.attempt == attempt }

	// Handle state transitions based on event type
	switch event.Type {
	case EventConnected:
		if err := c.transition(StateConnected, isCurrent); err != nil {
			c.emitError(fmt.Errorf("state transition failed: %w", err))
		}

	case EventDisconnected:
		c.setAddressingForAttempt(attempt, "", "")
		if err := c.transition(StateDisconnected, isCurrent); err != nil {
			c.emitError(fmt.Errorf("state transition failed: %w", err))
		}

	case EventGotIP:
		if ip := event.GetData("ip"); ip != "" {
			if !c.setAddressingForAttempt(attempt, ip, "") {
				return
			}
			// Detect the interface in background since it may take a moment to appear.
			// Verify state under lock before spawning to avoid unnecessary goroutines.
			c.mu.RLock()
			shouldDetect := c.assignedIP == ip && c.state != StateDisconnected
			c.mu.RUnlock()
			if shouldDetect {
				go c.detectInterface(attempt, ip)
			}
		}

	case EventError:
		c.emitError(errors.New(event.Message))
		// Only transition to Failed if this attempt is still in a connecting
		// state. If its process has already exited and transitioned to
		// Disconnected, there's no point in transitioning to Failed.
		transitioning := func(current ConnectionState) bool {
			return isCurrent(current) && current.IsTransitioning()
		}
		if err := c.transition(StateFailed, transitioning); err != nil {
			c.emitError(fmt.Errorf("state transition failed: %w", err))
		}

	case EventAuthenticate:
		if err := c.transition(StateAuthenticating, isCurrent); err != nil {
			c.emitError(fmt.Errorf("state transition failed: %w", err))
		}
	}
}

// buildCommandArgs constructs the command-line arguments for openfortivpn.
func (c *Controller) buildCommandArgs(p *profile.Profile, opts *ConnectOptions) []string {
	args := []string{
		fmt.Sprintf("%s:%d", p.Host, p.Port),
	}

	// Add username if using password or OTP authentication
	if (p.AuthMethod == profile.AuthMethodPassword || p.AuthMethod == profile.AuthMethodOTP) && p.Username != "" {
		args = append(args, "-u", p.Username)
	}

	// NOTE: the OTP is intentionally NOT passed via --otp. Command-line
	// arguments are world-readable through /proc/<pid>/cmdline, and the
	// token is live during the exact window an attacker would race. It is
	// delivered via stdin instead (see setupCredentialInput) — openfortivpn
	// reads the token from stdin when the gateway issues a 2FA challenge.

	// Add realm if specified
	if p.Realm != "" {
		args = append(args, fmt.Sprintf("--realm=%s", p.Realm))
	}

	// Add DNS setting
	if p.SetDNS {
		args = append(args, "--set-dns=1")
	} else {
		args = append(args, "--set-dns=0")
	}

	// Add routes setting
	if p.SetRoutes {
		args = append(args, "--set-routes=1")
	} else {
		args = append(args, "--set-routes=0")
	}

	// Add half-internet-routes setting (uses two /1 routes instead of replacing default route)
	if p.HalfInternetRoutes {
		args = append(args, "--half-internet-routes=1")
	} else {
		args = append(args, "--half-internet-routes=0")
	}

	// Add certificate authentication
	if p.AuthMethod == profile.AuthMethodCertificate {
		if p.ClientCertPath != "" {
			args = append(args, fmt.Sprintf("--user-cert=%s", p.ClientCertPath))
		}
		if p.ClientKeyPath != "" {
			args = append(args, fmt.Sprintf("--user-key=%s", p.ClientKeyPath))
		}
	}

	// Add SAML/SSO authentication
	if p.AuthMethod == profile.AuthMethodSAML {
		args = append(args, "--saml-login")
	}

	// Add trusted certificate hash
	if p.TrustedCert != "" {
		args = append(args, fmt.Sprintf("--trusted-cert=%s", p.TrustedCert))
	}

	return args
}

// Connect initiates a VPN connection using the given profile and options.
// openfortivpn requires root privileges to create network interfaces: in
// non-direct mode the command is run via pkexec for privilege escalation,
// while in direct mode (the helper daemon, already running as root) it is run
// directly. See startProcess.
//
// SECURITY: Password and OTP are passed via stdin, NOT command-line
// arguments. Command-line arguments are visible to all users via /proc or
// `ps aux`, which would expose credentials. Stdin is secure as it's only
// accessible by the process itself. NEVER pass secrets as CLI arguments.
func (c *Controller) Connect(ctx context.Context, p *profile.Profile, opts *ConnectOptions) error {
	if !c.CanConnect() {
		return fmt.Errorf("cannot connect: current state is %s", c.GetState())
	}

	// Validate profile before proceeding
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid profile: %w", err)
	}

	// Transition to connecting state
	attempt, err := c.beginAttempt()
	if err != nil {
		return fmt.Errorf("failed to set connecting state: %w", err)
	}

	// Handle nil options
	if opts == nil {
		opts = &ConnectOptions{}
	}

	// Start the VPN process
	process, cancel, err := c.startProcess(ctx, p, opts)
	if err != nil {
		return err
	}

	// Set up credential input (password and/or OTP) for non-SAML authentication
	c.setupCredentialInput(p, opts)

	// Set up stdout/stderr processing
	outputDone := c.setupOutputProcessing(attempt, process)

	// Handle process completion in background
	c.handleProcessCompletion(attempt, process, outputDone, cancel)

	return nil
}

// startProcess creates and starts the openfortivpn process: via pkexec normally,
// directly in direct mode (the helper daemon). Returns the process and the cancel
// func of the context driving it; on error the context is released and the state
// is set to Failed.
func (c *Controller) startProcess(ctx context.Context, p *profile.Profile, opts *ConnectOptions) (Process, context.CancelFunc, error) {
	// Create cancellable context. Stored under lock: other goroutines
	// (setupOutputProcessing, handleProcessCompletion) read these fields
	// under the same mutex.
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.ctx = ctx
	c.cancel = cancel
	c.mu.Unlock()

	// Build command arguments
	vpnArgs := c.buildCommandArgs(p, opts)

	// Create process - either directly or via pkexec
	var process Process
	var err error
	if c.directMode {
		// Direct mode: run openfortivpn directly (helper daemon already has root)
		process, err = c.executor.CreateProcess(ctx, c.openfortivpnPath, vpnArgs...)
	} else {
		// Normal mode: use pkexec for privilege escalation
		args := append([]string{c.openfortivpnPath}, vpnArgs...)
		process, err = c.executor.CreateProcess(ctx, "pkexec", args...)
	}
	if err != nil {
		c.mu.Lock()
		c.ctx = nil
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		if stateErr := c.setState(StateFailed); stateErr != nil {
			slog.Warn("Failed to set failed state", "error", stateErr)
		}
		return nil, nil, fmt.Errorf("failed to create process: %w", err)
	}

	c.mu.Lock()
	c.process = process
	c.stdin = process.Stdin()
	c.mu.Unlock()

	// Start the process
	if err := process.Start(); err != nil {
		c.mu.Lock()
		c.process = nil
		c.stdin = nil
		c.ctx = nil
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		if stateErr := c.setState(StateFailed); stateErr != nil {
			slog.Warn("Failed to set failed state", "error", stateErr)
		}
		return nil, nil, fmt.Errorf("failed to start openfortivpn: %w", err)
	}

	return process, cancel, nil
}

// setupCredentialInput writes the password and OTP to stdin, each on its own
// line: openfortivpn reads the password first, then — when the gateway issues
// a 2FA challenge — reads the one-time token from stdin as well. The pipe
// buffers the second line until the challenge arrives.
//
// SECURITY: Uses stdin (not CLI args) to prevent credential exposure in
// process listings. SAML authentication doesn't require credential input -
// credentials come from the browser.
func (c *Controller) setupCredentialInput(p *profile.Profile, opts *ConnectOptions) {
	if p.AuthMethod == profile.AuthMethodSAML {
		return
	}

	var payload string
	if opts.Password != "" {
		payload += opts.Password + "\n"
	}
	if opts.OTP != "" {
		payload += opts.OTP + "\n"
	}
	if payload == "" {
		return
	}

	// Capture stdin reference under lock before spawning goroutine.
	// This prevents a race where handleProcessCompletion nils c.stdin
	// before the goroutine can read it.
	c.mu.RLock()
	stdin := c.stdin
	c.mu.RUnlock()

	if stdin == nil {
		return
	}

	go func() {
		if _, err := stdin.Write([]byte(payload)); err != nil {
			c.emitError(fmt.Errorf("failed to write credentials to stdin: %w", err))
		}
	}()
}

// Scanner buffer sizing for openfortivpn output. The bufio.Scanner default
// caps tokens at 64 KiB; a single longer line would error the scanner and
// silently kill output processing for the rest of the connection, so the cap
// is raised explicitly.
const (
	outputScannerInitialSize = 64 * 1024
	outputScannerMaxLineSize = 1024 * 1024
)

// setupOutputProcessing starts goroutines to process stdout and stderr. The
// goroutines stop when the context is cancelled. The returned WaitGroup
// completes once both have finished reading, so callers can order work after
// the output.
func (c *Controller) setupOutputProcessing(attempt uint64, process Process) *sync.WaitGroup {
	c.mu.RLock()
	ctx := c.ctx
	c.mu.RUnlock()

	var outputDone sync.WaitGroup
	outputDone.Add(2)
	go func() {
		defer outputDone.Done()
		c.scanOutput(ctx, attempt, "stdout", process.Stdout())
	}()
	go func() {
		defer outputDone.Done()
		c.scanOutput(ctx, attempt, "stderr", process.Stderr())
	}()

	return &outputDone
}

// scanOutput reads one output stream line by line until EOF or context
// cancellation, forwarding each line to processOutput. Scanner errors are
// surfaced via emitError unless the context was cancelled (intentional
// shutdown); an ErrTooLong is reported explicitly since it means output was
// dropped despite the enlarged buffer.
func (c *Controller) scanOutput(ctx context.Context, attempt uint64, streamName string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, outputScannerInitialSize), outputScannerMaxLineSize)

	for scanner.Scan() {
		// Check if context was cancelled before processing output
		select {
		case <-ctx.Done():
			return
		default:
		}
		c.processOutput(attempt, scanner.Text())
	}

	// Scanner errors when pipe closes are expected during normal process exit
	// Don't emit errors if context was cancelled (intentional shutdown)
	err := scanner.Err()
	if err == nil || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	if errors.Is(err, bufio.ErrTooLong) {
		c.emitError(fmt.Errorf("%s line exceeded %d bytes; remaining output for this connection is lost", streamName, outputScannerMaxLineSize))
		return
	}
	c.emitError(fmt.Errorf("%s scanner error: %w", streamName, err))
}

// waitForOutputDrain blocks until the output scanners have finished reading, or
// until the drain timeout elapses.
func waitForOutputDrain(outputDone *sync.WaitGroup, timeout time.Duration) {
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		outputDone.Wait()
	}()

	select {
	case <-drained:
	case <-time.After(timeout):
		slog.Warn("Timed out waiting for openfortivpn output to drain; final output lines may be missing")
	}
}

// handleProcessCompletion waits for the process to exit however it was
// terminated, lets the output scanners finish, cleans up resources, and then
// transitions to disconnected. Draining before the state change is what keeps
// the final error line ahead of the terminal state the UI reacts to.
func (c *Controller) handleProcessCompletion(attempt uint64, process Process, outputDone *sync.WaitGroup, cancel context.CancelFunc) {
	go func() {
		// Wait error is intentionally ignored - we're cleaning up regardless
		_ = process.Wait()

		// The pipes outlive Wait (see newCmdWithPipes), so the scanners reach
		// the last line on their own; closing afterwards releases them even if
		// something else still holds the write ends.
		waitForOutputDrain(outputDone, c.outputDrainTimeout)
		_ = process.Stdout().Close()
		_ = process.Stderr().Close()

		// Release this connection's context. The process is already reaped, so
		// the context-driven kill is a no-op; what this stops is a scanner that
		// outlived the drain feeding stale lines to a newer connection.
		cancel()

		// A failure state re-enables Connect, so a newer attempt may already own
		// the controller. Touch nothing in that case: reporting a terminal state
		// would leave the live connection unstoppable from the UI. The attempt
		// is the test, not c.process — a retry claims it before it has a process.
		c.mu.Lock()
		owned := c.attempt == attempt
		if owned {
			c.process = nil
			c.ctx = nil
			c.cancel = nil
			if c.stdin != nil {
				if err := c.stdin.Close(); err != nil {
					slog.Warn("Failed to close stdin pipe", "error", err)
				}
			}
			c.stdin = nil
		}
		c.mu.Unlock()

		if !owned {
			slog.Debug("Skipping completion cleanup: a newer connection owns the controller")
			return
		}

		c.reportDisconnected(attempt)
	}()
}

// reportDisconnected moves a still-live connection to Disconnected. The attempt
// check shares the lock with the transition, because a scanner outliving the
// drain can fail this attempt — freeing the UI to start the next one — in the
// gap between the two.
func (c *Controller) reportDisconnected(attempt uint64) {
	c.dispatch.RLock()
	defer c.dispatch.RUnlock()

	live := func(current ConnectionState) bool {
		return c.attempt == attempt &&
			(current == StateConnected || current == StateConnecting || current == StateAuthenticating)
	}

	if err := c.transition(StateDisconnected, live); err != nil {
		slog.Warn("Failed to transition to disconnected state", "error", err)
	}
}

// Disconnect terminates the active VPN connection.
// Returns an error if the process cannot be killed (e.g., user cancelled
// the pkexec authentication dialog).
//
// Note: The context parameter is checked for pre-cancellation only (ctx.Err() at entry).
// It is not used to timeout or cancel the actual disconnect operations (process kill).
// This is intentional because disconnect operations should complete to ensure clean
// termination of the VPN process. The context parameter is provided for API consistency
// with the VPNController interface and to allow callers to skip disconnect if their
// context is already cancelled before the operation begins.
func (c *Controller) Disconnect(ctx context.Context) error {
	if !c.CanDisconnect() {
		return fmt.Errorf("not connected: current state is %s", c.GetState())
	}

	// Check if context is already cancelled (pre-cancellation check only)
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	cancel := c.cancel
	process := c.process
	c.mu.Unlock()

	// Cancel the context on every return path so its resources are always
	// released and any goroutine waiting on it unblocks — including the early
	// return below when Kill fails. Deferring runs it AFTER Kill: cancelling
	// first would SIGKILL the child immediately (the process is launched with
	// exec.CommandContext), defeating the graceful SIGTERM->grace->SIGKILL
	// escalation and preventing openfortivpn from tearing the tunnel down
	// cleanly. By the time cancel runs the process is already gone, so the
	// context-driven kill is a harmless no-op.
	if cancel != nil {
		defer cancel()
	}

	// Kill the process first so it gets the graceful escalation (see Process.Kill).
	if process != nil {
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to kill VPN process: %w", err)
		}
	}

	return nil
}
