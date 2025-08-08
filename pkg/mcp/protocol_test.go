package mcp

import (
	"encoding/json"
	"testing"
)

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Message
		wantErr bool
	}{
		{
			name: "valid request",
			input: `{"jsonrpc":"2.0","id":1,"method":"test","params":{"key":"value"}}`,
			want: &Message{
				JSONRPC: "2.0",
				ID:      jsonRawMessage("1"),
				Method:  "test",
				Params:  json.RawMessage(`{"key":"value"}`),
			},
			wantErr: false,
		},
		{
			name: "valid notification",
			input: `{"jsonrpc":"2.0","method":"notify","params":null}`,
			want: &Message{
				JSONRPC: "2.0",
				Method:  "notify",
				Params:  json.RawMessage("null"),
			},
			wantErr: false,
		},
		{
			name: "valid response",
			input: `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}`,
			want: &Message{
				JSONRPC: "2.0",
				ID:      jsonRawMessage("1"),
				Result:  json.RawMessage(`{"status":"ok"}`),
			},
			wantErr: false,
		},
		{
			name: "error response",
			input: `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			want: &Message{
				JSONRPC: "2.0",
				ID:      jsonRawMessage("1"),
				Error: &Error{
					Code:    -32600,
					Message: "Invalid Request",
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid json}`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   ``,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMessage([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.JSONRPC != tt.want.JSONRPC {
					t.Errorf("JSONRPC = %v, want %v", got.JSONRPC, tt.want.JSONRPC)
				}
				if got.Method != tt.want.Method {
					t.Errorf("Method = %v, want %v", got.Method, tt.want.Method)
				}
				if (got.ID == nil) != (tt.want.ID == nil) {
					t.Errorf("ID nil mismatch")
				}
				if (got.Error == nil) != (tt.want.Error == nil) {
					t.Errorf("Error nil mismatch")
				}
			}
		})
	}
}

func TestMessage_Marshal(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantErr bool
	}{
		{
			name: "request message",
			msg: &Message{
				JSONRPC: "2.0",
				ID:      jsonRawMessage("1"),
				Method:  "test",
				Params:  json.RawMessage(`{"key":"value"}`),
			},
			wantErr: false,
		},
		{
			name: "response message",
			msg: &Message{
				JSONRPC: "2.0",
				ID:      jsonRawMessage("1"),
				Result:  json.RawMessage(`{"status":"ok"}`),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.msg.Marshal()
			if (err != nil) != tt.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) == 0 {
				t.Errorf("Marshal() returned empty bytes")
			}
			
			// Verify round-trip
			if !tt.wantErr {
				parsed, err := ParseMessage(got)
				if err != nil {
					t.Errorf("Round-trip parse failed: %v", err)
				}
				if parsed.Method != tt.msg.Method {
					t.Errorf("Round-trip method mismatch")
				}
			}
		})
	}
}

func TestMessage_Types(t *testing.T) {
	tests := []struct {
		name           string
		msg            *Message
		isRequest      bool
		isNotification bool
		isResponse     bool
	}{
		{
			name: "request",
			msg: &Message{
				Method: "test",
				ID:     jsonRawMessage("1"),
			},
			isRequest:      true,
			isNotification: false,
			isResponse:     false,
		},
		{
			name: "notification",
			msg: &Message{
				Method: "notify",
				ID:     nil,
			},
			isRequest:      false,
			isNotification: true,
			isResponse:     false,
		},
		{
			name: "response",
			msg: &Message{
				ID:     jsonRawMessage("1"),
				Result: json.RawMessage(`{}`),
			},
			isRequest:      false,
			isNotification: false,
			isResponse:     true,
		},
		{
			name: "error response",
			msg: &Message{
				ID:    jsonRawMessage("1"),
				Error: &Error{Code: -32600, Message: "error"},
			},
			isRequest:      false,
			isNotification: false,
			isResponse:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.IsRequest(); got != tt.isRequest {
				t.Errorf("IsRequest() = %v, want %v", got, tt.isRequest)
			}
			if got := tt.msg.IsNotification(); got != tt.isNotification {
				t.Errorf("IsNotification() = %v, want %v", got, tt.isNotification)
			}
			if got := tt.msg.IsResponse(); got != tt.isResponse {
				t.Errorf("IsResponse() = %v, want %v", got, tt.isResponse)
			}
		})
	}
}

func TestTransportType_String(t *testing.T) {
	tests := []struct {
		transport TransportType
		want      string
	}{
		{TransportAuto, "auto"},
		{TransportHTTP, "http"},
		{TransportSSE, "sse"},
		{TransportWebSocket, "websocket"},
		{TransportType(99), "auto"}, // Unknown type
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.transport.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to create json.RawMessage from string
func jsonRawMessage(s string) *json.RawMessage {
	raw := json.RawMessage(s)
	return &raw
}

// Benchmark tests
func BenchmarkParseMessage(b *testing.B) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"test","params":{"key":"value"}}`)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseMessage(data)
	}
}

func BenchmarkMarshalMessage(b *testing.B) {
	msg := &Message{
		JSONRPC: "2.0",
		ID:      jsonRawMessage("1"),
		Method:  "test",
		Params:  json.RawMessage(`{"key":"value"}`),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = msg.Marshal()
	}
}