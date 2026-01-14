// Package protocol defines the message types for communication between
// the openfortivpn-gui application and the privileged helper daemon.
package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRequest tests the NewRequest constructor function.
func TestNewRequest(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		cmd     Command
		params  interface{}
		wantErr bool
	}{
		{
			name: "create connect request with params",
			id:   "req-001",
			cmd:  CommandConnect,
			params: ConnectParams{
				ProfileID:  "profile-123",
				Host:       "vpn.example.com",
				Port:       443,
				Username:   "testuser",
				AuthMethod: "password",
			},
			wantErr: false,
		},
		{
			name:    "create disconnect request with empty params",
			id:      "req-002",
			cmd:     CommandDisconnect,
			params:  DisconnectParams{},
			wantErr: false,
		},
		{
			name:    "create status request with empty params",
			id:      "req-003",
			cmd:     CommandStatus,
			params:  StatusParams{},
			wantErr: false,
		},
		{
			name:    "create request with nil params",
			id:      "req-004",
			cmd:     CommandStatus,
			params:  nil,
			wantErr: false,
		},
		{
			name:   "create request with complex nested params",
			id:     "req-005",
			cmd:    CommandConnect,
			params: map[string]interface{}{"nested": map[string]string{"key": "value"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := NewRequest(tt.id, tt.cmd, tt.params)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, req)

			assert.Equal(t, tt.id, req.ID)
			assert.Equal(t, MessageTypeRequest, req.Type)
			assert.Equal(t, tt.cmd, req.Command)
			assert.NotNil(t, req.Params)

			// Verify params can be unmarshaled back
			if tt.params != nil {
				var decoded json.RawMessage
				err = json.Unmarshal(req.Params, &decoded)
				require.NoError(t, err)
			}
		})
	}
}

// TestNewRequest_ParamsMarshaling verifies that parameters are correctly marshaled to JSON.
func TestNewRequest_ParamsMarshaling(t *testing.T) {
	params := ConnectParams{
		ProfileID:          "test-profile",
		Host:               "vpn.example.com",
		Port:               8443,
		Username:           "admin",
		Password:           "secret123",
		AuthMethod:         "password",
		TrustedCert:        "abc123",
		ClientCertPath:     "/path/to/cert.pem",
		ClientKeyPath:      "/path/to/key.pem",
		SetDNS:             true,
		SetRoutes:          false,
		HalfInternetRoutes: true,
	}

	req, err := NewRequest("test-id", CommandConnect, params)
	require.NoError(t, err)

	// Unmarshal params back to verify
	var decoded ConnectParams
	err = json.Unmarshal(req.Params, &decoded)
	require.NoError(t, err)

	assert.Equal(t, params.ProfileID, decoded.ProfileID)
	assert.Equal(t, params.Host, decoded.Host)
	assert.Equal(t, params.Port, decoded.Port)
	assert.Equal(t, params.Username, decoded.Username)
	assert.Equal(t, params.Password, decoded.Password)
	assert.Equal(t, params.AuthMethod, decoded.AuthMethod)
	assert.Equal(t, params.TrustedCert, decoded.TrustedCert)
	assert.Equal(t, params.ClientCertPath, decoded.ClientCertPath)
	assert.Equal(t, params.ClientKeyPath, decoded.ClientKeyPath)
	assert.Equal(t, params.SetDNS, decoded.SetDNS)
	assert.Equal(t, params.SetRoutes, decoded.SetRoutes)
	assert.Equal(t, params.HalfInternetRoutes, decoded.HalfInternetRoutes)
}

// TestNewSuccessResponse tests the NewSuccessResponse constructor function.
func TestNewSuccessResponse(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		result  interface{}
		wantErr bool
	}{
		{
			name: "success response with status result",
			id:   "resp-001",
			result: StatusResult{
				State:              "connected",
				AssignedIP:         "10.0.0.5",
				ConnectedProfileID: "profile-123",
			},
			wantErr: false,
		},
		{
			name:    "success response with nil result",
			id:      "resp-002",
			result:  nil,
			wantErr: false,
		},
		{
			name:    "success response with empty struct",
			id:      "resp-003",
			result:  struct{}{},
			wantErr: false,
		},
		{
			name:    "success response with string result",
			id:      "resp-004",
			result:  "operation completed",
			wantErr: false,
		},
		{
			name:    "success response with map result",
			id:      "resp-005",
			result:  map[string]string{"key": "value"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := NewSuccessResponse(tt.id, tt.result)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, tt.id, resp.ID)
			assert.Equal(t, MessageTypeResponse, resp.Type)
			assert.True(t, resp.Success)
			assert.Nil(t, resp.Error)

			if tt.result != nil {
				assert.NotNil(t, resp.Result)
			}
		})
	}
}

// TestNewSuccessResponse_ResultMarshaling verifies that results are correctly marshaled.
func TestNewSuccessResponse_ResultMarshaling(t *testing.T) {
	result := StatusResult{
		State:              "connected",
		AssignedIP:         "192.168.1.100",
		ConnectedProfileID: "my-vpn-profile",
	}

	resp, err := NewSuccessResponse("test-id", result)
	require.NoError(t, err)

	// Unmarshal result back to verify
	var decoded StatusResult
	err = json.Unmarshal(resp.Result, &decoded)
	require.NoError(t, err)

	assert.Equal(t, result.State, decoded.State)
	assert.Equal(t, result.AssignedIP, decoded.AssignedIP)
	assert.Equal(t, result.ConnectedProfileID, decoded.ConnectedProfileID)
}

// TestNewErrorResponse tests the NewErrorResponse constructor function.
func TestNewErrorResponse(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		code    string
		message string
	}{
		{
			name:    "error response with invalid command",
			id:      "err-001",
			code:    ErrCodeInvalidCommand,
			message: "unknown command: foo",
		},
		{
			name:    "error response with connection failed",
			id:      "err-002",
			code:    ErrCodeConnectionFailed,
			message: "failed to establish tunnel",
		},
		{
			name:    "error response with invalid params",
			id:      "err-003",
			code:    ErrCodeInvalidParams,
			message: "missing required parameter: host",
		},
		{
			name:    "error response with invalid state",
			id:      "err-004",
			code:    ErrCodeInvalidState,
			message: "cannot disconnect: not connected",
		},
		{
			name:    "error response with empty message",
			id:      "err-005",
			code:    ErrCodeInternalError,
			message: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := NewErrorResponse(tt.id, tt.code, tt.message)

			require.NotNil(t, resp)

			assert.Equal(t, tt.id, resp.ID)
			assert.Equal(t, MessageTypeResponse, resp.Type)
			assert.False(t, resp.Success)
			assert.Nil(t, resp.Result)

			require.NotNil(t, resp.Error)
			assert.Equal(t, tt.code, resp.Error.Code)
			assert.Equal(t, tt.message, resp.Error.Message)
		})
	}
}

// TestNewEvent tests the NewEvent constructor function.
func TestNewEvent(t *testing.T) {
	tests := []struct {
		name    string
		evtName EventName
		data    interface{}
		wantErr bool
	}{
		{
			name:    "state change event",
			evtName: EventStateChange,
			data: StateChangeData{
				From: "disconnected",
				To:   "connecting",
			},
			wantErr: false,
		},
		{
			name:    "output event",
			evtName: EventOutput,
			data: OutputData{
				Line: "Tunnel is up and running",
			},
			wantErr: false,
		},
		{
			name:    "vpn event",
			evtName: EventVPN,
			data: VPNEventData{
				EventType: "got_ip",
				Message:   "Got assigned IP: 10.0.0.5",
				Data:      map[string]string{"ip": "10.0.0.5"},
			},
			wantErr: false,
		},
		{
			name:    "error event",
			evtName: EventError,
			data: ErrorData{
				Message: "Connection timed out",
			},
			wantErr: false,
		},
		{
			name:    "event with nil data",
			evtName: EventOutput,
			data:    nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt, err := NewEvent(tt.evtName, tt.data)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, evt)

			assert.Equal(t, MessageTypeEvent, evt.Type)
			assert.Equal(t, tt.evtName, evt.Name)
			assert.NotNil(t, evt.Data)
		})
	}
}

// TestNewEvent_DataMarshaling verifies that event data is correctly marshaled.
func TestNewEvent_DataMarshaling(t *testing.T) {
	t.Run("state change data", func(t *testing.T) {
		data := StateChangeData{
			From: "connecting",
			To:   "connected",
		}

		evt, err := NewEvent(EventStateChange, data)
		require.NoError(t, err)

		var decoded StateChangeData
		err = json.Unmarshal(evt.Data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, data.From, decoded.From)
		assert.Equal(t, data.To, decoded.To)
	})

	t.Run("output data", func(t *testing.T) {
		data := OutputData{
			Line: "DNS server: 8.8.8.8",
		}

		evt, err := NewEvent(EventOutput, data)
		require.NoError(t, err)

		var decoded OutputData
		err = json.Unmarshal(evt.Data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, data.Line, decoded.Line)
	})

	t.Run("vpn event data", func(t *testing.T) {
		data := VPNEventData{
			EventType: "authenticate",
			Message:   "Authentication successful",
			Data:      map[string]string{"user": "admin", "method": "saml"},
		}

		evt, err := NewEvent(EventVPN, data)
		require.NoError(t, err)

		var decoded VPNEventData
		err = json.Unmarshal(evt.Data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, data.EventType, decoded.EventType)
		assert.Equal(t, data.Message, decoded.Message)
		assert.Equal(t, data.Data, decoded.Data)
	})

	t.Run("error data", func(t *testing.T) {
		data := ErrorData{
			Message: "Network unreachable",
		}

		evt, err := NewEvent(EventError, data)
		require.NoError(t, err)

		var decoded ErrorData
		err = json.Unmarshal(evt.Data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, data.Message, decoded.Message)
	})
}

// TestMessageTypes verifies the message type constants are correct.
func TestMessageTypes(t *testing.T) {
	assert.Equal(t, MessageType("request"), MessageTypeRequest)
	assert.Equal(t, MessageType("response"), MessageTypeResponse)
	assert.Equal(t, MessageType("event"), MessageTypeEvent)
}

// TestCommands verifies the command constants are correct.
func TestCommands(t *testing.T) {
	assert.Equal(t, Command("connect"), CommandConnect)
	assert.Equal(t, Command("disconnect"), CommandDisconnect)
	assert.Equal(t, Command("status"), CommandStatus)
}

// TestEventNames verifies the event name constants are correct.
func TestEventNames(t *testing.T) {
	assert.Equal(t, EventName("state_change"), EventStateChange)
	assert.Equal(t, EventName("output"), EventOutput)
	assert.Equal(t, EventName("vpn_event"), EventVPN)
	assert.Equal(t, EventName("error"), EventError)
}

// TestRequest_JSONSerialization tests that requests can be serialized and deserialized.
func TestRequest_JSONSerialization(t *testing.T) {
	original, err := NewRequest("test-123", CommandConnect, ConnectParams{
		ProfileID: "profile-1",
		Host:      "vpn.test.com",
		Port:      443,
	})
	require.NoError(t, err)

	// Serialize
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Deserialize
	var decoded Request
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Command, decoded.Command)
}

// TestResponse_JSONSerialization tests that responses can be serialized and deserialized.
func TestResponse_JSONSerialization(t *testing.T) {
	t.Run("success response", func(t *testing.T) {
		original, err := NewSuccessResponse("resp-123", StatusResult{
			State:      "connected",
			AssignedIP: "10.1.2.3",
		})
		require.NoError(t, err)

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded Response
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Type, decoded.Type)
		assert.Equal(t, original.Success, decoded.Success)
		assert.Nil(t, decoded.Error)
	})

	t.Run("error response", func(t *testing.T) {
		original := NewErrorResponse("resp-456", ErrCodeConnectionFailed, "timeout")

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded Response
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Type, decoded.Type)
		assert.False(t, decoded.Success)
		require.NotNil(t, decoded.Error)
		assert.Equal(t, original.Error.Code, decoded.Error.Code)
		assert.Equal(t, original.Error.Message, decoded.Error.Message)
	})
}

// TestEvent_JSONSerialization tests that events can be serialized and deserialized.
func TestEvent_JSONSerialization(t *testing.T) {
	original, err := NewEvent(EventStateChange, StateChangeData{
		From: "disconnected",
		To:   "connected",
	})
	require.NoError(t, err)

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Event
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Name, decoded.Name)
}
