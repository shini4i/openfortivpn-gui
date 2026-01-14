package vpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

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

// Controller manages VPN connection lifecycle using openfortivpn.
type Controller struct {
	openfortivpnPath string
	executor         ProcessExecutor
	directMode       bool // When true, run openfortivpn directly without pkexec

	mu         sync.RWMutex
	state      ConnectionState
	assignedIP string

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

// NewController creates a new VPN controller instance.
func NewController(openfortivpnPath string) *Controller {
	return &Controller{
		openfortivpnPath: openfortivpnPath,
		executor:         NewRealExecutor(),
		state:            StateDisconnected,
	}
}

// NewControllerWithExecutor creates a new VPN controller with a custom executor.
// This is primarily used for testing.
func NewControllerWithExecutor(openfortivpnPath string, executor ProcessExecutor) *Controller {
	return &Controller{
		openfortivpnPath: openfortivpnPath,
		executor:         executor,
		state:            StateDisconnected,
	}
}

// NewControllerDirect creates a VPN controller that runs openfortivpn directly
// without pkexec privilege escalation. This is intended for use by the helper
// daemon which already runs with root privileges.
func NewControllerDirect(openfortivpnPath string) *Controller {
	return &Controller{
		openfortivpnPath: openfortivpnPath,
		executor:         NewDirectExecutor(),
		directMode:       true,
		state:            StateDisconnected,
	}
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
	c.mu.Lock()
	if !IsValidTransition(c.state, newState) {
		c.mu.Unlock()
		return fmt.Errorf("invalid state transition from %s to %s", c.state, newState)
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

// processOutput processes a single line of openfortivpn output.
func (c *Controller) processOutput(line string) {
	// Emit raw output
	c.emitOutput(line)

	// Parse the line
	event := ParseLine(line)
	if event == nil {
		return
	}

	// Emit parsed event
	c.emitEvent(event)

	// Handle state transitions based on event type
	switch event.Type {
	case EventConnected:
		if err := c.setState(StateConnected); err != nil {
			c.emitError(fmt.Errorf("state transition failed: %w", err))
		}

	case EventDisconnected:
		c.setAssignedIP("")
		if err := c.setState(StateDisconnected); err != nil {
			c.emitError(fmt.Errorf("state transition failed: %w", err))
		}

	case EventGotIP:
		if ip := event.GetData("ip"); ip != "" {
			c.setAssignedIP(ip)
		}

	case EventError:
		c.emitError(errors.New(event.Message))
		// Only transition to Failed if we're still in a connecting state.
		// If the process has already exited and transitioned to Disconnected,
		// there's no point in transitioning to Failed.
		currentState := c.GetState()
		if currentState.IsTransitioning() {
			if err := c.setState(StateFailed); err != nil {
				c.emitError(fmt.Errorf("state transition failed: %w", err))
			}
		}

	case EventAuthenticate:
		if err := c.setState(StateAuthenticating); err != nil {
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

	// Add OTP if provided (for two-factor authentication)
	if opts != nil && opts.OTP != "" {
		args = append(args, fmt.Sprintf("--otp=%s", opts.OTP))
	}

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
// The command is run via pkexec for privilege escalation since openfortivpn
// requires root privileges to create network interfaces.
//
// SECURITY: Password is passed via stdin, NOT command-line arguments.
// Command-line arguments are visible to all users via /proc or `ps aux`,
// which would expose credentials. Stdin is secure as it's only accessible
// by the process itself. NEVER pass passwords as CLI arguments.
//
// Note: OTP is passed via --otp flag as it's time-limited and single-use,
// making command-line exposure an acceptable trade-off for implementation simplicity.
func (c *Controller) Connect(ctx context.Context, p *profile.Profile, opts *ConnectOptions) error {
	if !c.CanConnect() {
		return fmt.Errorf("cannot connect: current state is %s", c.GetState())
	}

	// Validate profile before proceeding
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid profile: %w", err)
	}

	// Transition to connecting state
	if err := c.setState(StateConnecting); err != nil {
		return fmt.Errorf("failed to set connecting state: %w", err)
	}

	// Handle nil options
	if opts == nil {
		opts = &ConnectOptions{}
	}

	// Start the VPN process
	process, err := c.startProcess(ctx, p, opts)
	if err != nil {
		return err
	}

	// Set up password input for non-SAML authentication
	c.setupPasswordInput(p, opts.Password)

	// Set up stdout/stderr processing
	c.setupOutputProcessing(process)

	// Handle process completion in background
	c.handleProcessCompletion(process)

	return nil
}

// startProcess creates and starts the openfortivpn process.
// In normal mode, it uses pkexec for privilege escalation.
// In direct mode (helper daemon), it runs openfortivpn directly.
// Returns the started process or an error. On error, the state is set to Failed.
func (c *Controller) startProcess(ctx context.Context, p *profile.Profile, opts *ConnectOptions) (Process, error) {
	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	c.ctx = ctx
	c.cancel = cancel

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
		c.ctx = nil
		c.cancel = nil
		if stateErr := c.setState(StateFailed); stateErr != nil {
			slog.Warn("Failed to set failed state", "error", stateErr)
		}
		return nil, fmt.Errorf("failed to create process: %w", err)
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
		c.mu.Unlock()
		c.ctx = nil
		c.cancel = nil
		if stateErr := c.setState(StateFailed); stateErr != nil {
			slog.Warn("Failed to set failed state", "error", stateErr)
		}
		return nil, fmt.Errorf("failed to start openfortivpn: %w", err)
	}

	return process, nil
}

// setupPasswordInput writes the password to stdin for password-based authentication.
// SECURITY: Uses stdin (not CLI args) to prevent password exposure in process listings.
// SAML authentication doesn't require password input - credentials come from browser.
func (c *Controller) setupPasswordInput(p *profile.Profile, password string) {
	if p.AuthMethod == profile.AuthMethodSAML || password == "" {
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
		if _, err := stdin.Write([]byte(password + "\n")); err != nil {
			c.emitError(fmt.Errorf("failed to write password to stdin: %w", err))
		}
	}()
}

// setupOutputProcessing starts goroutines to process stdout and stderr.
// The goroutines respect context cancellation and will stop processing
// when the context is cancelled.
func (c *Controller) setupOutputProcessing(process Process) {
	c.mu.RLock()
	ctx := c.ctx
	c.mu.RUnlock()

	// Process stdout
	go func() {
		scanner := bufio.NewScanner(process.Stdout())
		for scanner.Scan() {
			// Check if context was cancelled before processing output
			select {
			case <-ctx.Done():
				return
			default:
			}
			c.processOutput(scanner.Text())
		}
		// Scanner errors when pipe closes are expected during normal process exit
		// Don't emit errors if context was cancelled (intentional shutdown)
		if err := scanner.Err(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			select {
			case <-ctx.Done():
				return
			default:
				c.emitError(fmt.Errorf("stdout scanner error: %w", err))
			}
		}
	}()

	// Process stderr
	go func() {
		scanner := bufio.NewScanner(process.Stderr())
		for scanner.Scan() {
			// Check if context was cancelled before processing output
			select {
			case <-ctx.Done():
				return
			default:
			}
			c.processOutput(scanner.Text())
		}
		// Scanner errors when pipe closes are expected during normal process exit
		// Don't emit errors if context was cancelled (intentional shutdown)
		if err := scanner.Err(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			select {
			case <-ctx.Done():
				return
			default:
				c.emitError(fmt.Errorf("stderr scanner error: %w", err))
			}
		}
	}()
}

// handleProcessCompletion waits for the process to exit and cleans up resources.
// It always transitions to disconnected state when the process exits, regardless
// of whether the context was cancelled (intentional disconnect) or the process
// exited on its own.
func (c *Controller) handleProcessCompletion(process Process) {
	go func() {
		// Wait error is intentionally ignored - we're cleaning up regardless
		_ = process.Wait()

		c.mu.Lock()
		c.process = nil
		c.ctx = nil
		c.cancel = nil
		if c.stdin != nil {
			if err := c.stdin.Close(); err != nil {
				slog.Warn("Failed to close stdin pipe", "error", err)
			}
		}
		c.stdin = nil
		currentState := c.state
		c.mu.Unlock()

		// Transition to disconnected if we're still in a connected/connecting state
		if currentState == StateConnected || currentState == StateConnecting || currentState == StateAuthenticating {
			if err := c.setState(StateDisconnected); err != nil {
				slog.Warn("Failed to transition to disconnected state", "error", err)
			}
		}
	}()
}

// Disconnect terminates the active VPN connection.
// Returns an error if the process cannot be killed (e.g., user cancelled
// the pkexec authentication dialog).
func (c *Controller) Disconnect(ctx context.Context) error {
	if !c.CanDisconnect() {
		return fmt.Errorf("not connected: current state is %s", c.GetState())
	}

	// Check if context is already cancelled
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	cancel := c.cancel
	process := c.process
	c.mu.Unlock()

	// Cancel context to signal termination
	if cancel != nil {
		cancel()
	}

	// Kill process if still running
	if process != nil {
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to kill VPN process: %w", err)
		}
	}

	return nil
}
