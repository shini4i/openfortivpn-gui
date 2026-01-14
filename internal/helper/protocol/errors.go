package protocol

// Error codes for protocol responses.
const (
	// ErrCodeInvalidRequest indicates the request was malformed.
	ErrCodeInvalidRequest = "INVALID_REQUEST"
	// ErrCodeInvalidCommand indicates an unknown command was sent.
	ErrCodeInvalidCommand = "INVALID_COMMAND"
	// ErrCodeInvalidParams indicates the command parameters were invalid.
	ErrCodeInvalidParams = "INVALID_PARAMS"
	// ErrCodeInvalidState indicates the operation is not allowed in the current state.
	ErrCodeInvalidState = "INVALID_STATE"
	// ErrCodeConnectionFailed indicates the VPN connection failed.
	ErrCodeConnectionFailed = "CONNECTION_FAILED"
	// ErrCodeDisconnectFailed indicates the VPN disconnection failed.
	ErrCodeDisconnectFailed = "DISCONNECT_FAILED"
	// ErrCodeInternalError indicates an unexpected internal error.
	ErrCodeInternalError = "INTERNAL_ERROR"
	// ErrCodeProfileInvalid indicates the profile configuration is invalid.
	ErrCodeProfileInvalid = "PROFILE_INVALID"
)
