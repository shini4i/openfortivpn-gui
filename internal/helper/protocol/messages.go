// Package protocol defines the message types for communication between
// the openfortivpn-gui application and the privileged helper daemon.
//
// The protocol uses newline-delimited JSON (NDJSON) format over a UNIX socket.
// Each message is a single JSON object terminated by a newline character.
package protocol

import (
	"encoding/json"
)

// MessageType identifies the type of message.
type MessageType string

const (
	// MessageTypeRequest is sent from client to server.
	MessageTypeRequest MessageType = "request"
	// MessageTypeResponse is sent from server to client in reply to a request.
	MessageTypeResponse MessageType = "response"
	// MessageTypeEvent is broadcast from server to all connected clients.
	MessageTypeEvent MessageType = "event"
)

// Command identifies the operation to perform.
type Command string

const (
	// CommandConnect initiates a VPN connection.
	CommandConnect Command = "connect"
	// CommandDisconnect terminates the active VPN connection.
	CommandDisconnect Command = "disconnect"
	// CommandStatus queries the current VPN status.
	CommandStatus Command = "status"
)

// EventName identifies the type of event.
type EventName string

const (
	// EventStateChange indicates a VPN connection state transition.
	EventStateChange EventName = "state_change"
	// EventOutput contains a line of output from openfortivpn.
	EventOutput EventName = "output"
	// EventVPN contains a parsed VPN event (got_ip, authenticate, etc.).
	EventVPN EventName = "vpn_event"
	// EventError indicates an error occurred.
	EventError EventName = "error"
)

// Request represents a command sent from client to server.
type Request struct {
	// ID is a unique identifier for correlating responses.
	ID string `json:"id"`
	// Type is always "request".
	Type MessageType `json:"type"`
	// Command is the operation to perform.
	Command Command `json:"command"`
	// Params contains command-specific parameters.
	Params json.RawMessage `json:"params"`
}

// Response represents a reply from server to client.
type Response struct {
	// ID matches the request ID.
	ID string `json:"id"`
	// Type is always "response".
	Type MessageType `json:"type"`
	// Success indicates whether the command succeeded.
	Success bool `json:"success"`
	// Result contains command-specific result data (if Success is true).
	Result json.RawMessage `json:"result,omitempty"`
	// Error contains error details (if Success is false).
	Error *ErrorInfo `json:"error,omitempty"`
}

// Event represents an asynchronous notification from server to clients.
type Event struct {
	// Type is always "event".
	Type MessageType `json:"type"`
	// Name identifies the event type.
	Name EventName `json:"name"`
	// Data contains event-specific information.
	Data json.RawMessage `json:"data"`
}

// ErrorInfo contains details about an error.
type ErrorInfo struct {
	// Code is a machine-readable error code.
	Code string `json:"code"`
	// Message is a human-readable error description.
	Message string `json:"message"`
}

// ConnectParams contains parameters for the connect command.
type ConnectParams struct {
	// ProfileID is the unique identifier of the profile.
	ProfileID string `json:"profile_id"`
	// Host is the VPN server hostname or IP.
	Host string `json:"host"`
	// Port is the VPN server port.
	Port int `json:"port"`
	// Username for authentication.
	Username string `json:"username"`
	// Password for authentication (only for password/OTP auth methods).
	Password string `json:"password,omitempty"`
	// OTP is the one-time password for 2FA.
	OTP string `json:"otp,omitempty"`
	// AuthMethod is the authentication method (password, otp, certificate, saml).
	AuthMethod string `json:"auth_method"`
	// Realm for SAML authentication.
	Realm string `json:"realm,omitempty"`
	// TrustedCert is the server certificate hash for validation.
	TrustedCert string `json:"trusted_cert,omitempty"`
	// ClientCertPath is the path to the client certificate file.
	ClientCertPath string `json:"client_cert_path,omitempty"`
	// ClientKeyPath is the path to the client private key file.
	ClientKeyPath string `json:"client_key_path,omitempty"`
	// SetDNS controls whether to configure DNS via VPN.
	SetDNS bool `json:"set_dns"`
	// SetRoutes controls whether to configure routes via VPN.
	SetRoutes bool `json:"set_routes"`
	// HalfInternetRoutes uses /1 routes instead of default route.
	HalfInternetRoutes bool `json:"half_internet_routes"`
}

// DisconnectParams contains parameters for the disconnect command.
// Currently empty but defined for future extensibility.
type DisconnectParams struct{}

// StatusParams contains parameters for the status command.
// Currently empty but defined for future extensibility.
type StatusParams struct{}

// StatusResult contains the result of a status query.
type StatusResult struct {
	// State is the current connection state.
	State string `json:"state"`
	// AssignedIP is the IP assigned by the VPN server (empty if not connected).
	AssignedIP string `json:"assigned_ip,omitempty"`
	// ConnectedProfileID is the ID of the currently connected profile.
	ConnectedProfileID string `json:"connected_profile_id,omitempty"`
}

// StateChangeData contains data for state_change events.
type StateChangeData struct {
	// From is the previous state.
	From string `json:"from"`
	// To is the new state.
	To string `json:"to"`
}

// OutputData contains data for output events.
type OutputData struct {
	// Line is a single line of output from openfortivpn.
	Line string `json:"line"`
}

// VPNEventData contains data for vpn_event events.
type VPNEventData struct {
	// EventType is the type of VPN event (got_ip, authenticate, etc.).
	EventType string `json:"event_type"`
	// Message is the original output line that triggered the event.
	Message string `json:"message,omitempty"`
	// Data contains event-specific key-value pairs.
	Data map[string]string `json:"data,omitempty"`
}

// ErrorData contains data for error events.
type ErrorData struct {
	// Message is the error description.
	Message string `json:"message"`
}

// NewRequest creates a new request with the given command and parameters.
func NewRequest(id string, cmd Command, params interface{}) (*Request, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &Request{
		ID:      id,
		Type:    MessageTypeRequest,
		Command: cmd,
		Params:  paramsJSON,
	}, nil
}

// NewSuccessResponse creates a successful response.
func NewSuccessResponse(id string, result interface{}) (*Response, error) {
	var resultJSON json.RawMessage
	if result != nil {
		var err error
		resultJSON, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
	}
	return &Response{
		ID:      id,
		Type:    MessageTypeResponse,
		Success: true,
		Result:  resultJSON,
	}, nil
}

// NewErrorResponse creates an error response.
func NewErrorResponse(id string, code string, message string) *Response {
	return &Response{
		ID:      id,
		Type:    MessageTypeResponse,
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	}
}

// NewEvent creates a new event with the given name and data.
func NewEvent(name EventName, data interface{}) (*Event, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Event{
		Type: MessageTypeEvent,
		Name: name,
		Data: dataJSON,
	}, nil
}
