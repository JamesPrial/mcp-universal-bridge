package mcp

import (
	"encoding/json"
	"fmt"
)

// Message represents a JSON-RPC 2.0 message
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// TransportType represents the communication transport
type TransportType int

const (
	TransportAuto TransportType = iota
	TransportHTTP
	TransportSSE
	TransportWebSocket
)

func (t TransportType) String() string {
	switch t {
	case TransportHTTP:
		return "http"
	case TransportSSE:
		return "sse"
	case TransportWebSocket:
		return "websocket"
	default:
		return "auto"
	}
}

// UnmarshalYAML implements yaml.Unmarshaler for TransportType
func (t *TransportType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	
	switch s {
	case "auto":
		*t = TransportAuto
	case "http":
		*t = TransportHTTP
	case "sse":
		*t = TransportSSE
	case "websocket":
		*t = TransportWebSocket
	default:
		return fmt.Errorf("unknown transport type: %s", s)
	}
	
	return nil
}

// MarshalYAML implements yaml.Marshaler for TransportType
func (t TransportType) MarshalYAML() (interface{}, error) {
	return t.String(), nil
}

// ParseMessage parses a JSON-RPC message from bytes
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}
	return &msg, nil
}

// Marshal converts a message to JSON bytes
func (m *Message) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// IsRequest returns true if this is a request message
func (m *Message) IsRequest() bool {
	return m.Method != "" && m.ID != nil
}

// IsNotification returns true if this is a notification message
func (m *Message) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// IsResponse returns true if this is a response message
func (m *Message) IsResponse() bool {
	return m.Method == "" && m.ID != nil
}