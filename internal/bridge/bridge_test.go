package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jamesprial/mcp-universal-bridge/internal/config"
	"github.com/jamesprial/mcp-universal-bridge/internal/transport"
	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
)

// MockTransport implements RemoteTransport for testing
type MockTransport struct {
	connected bool
	messages  chan *mcp.Message
	outbox    chan *mcp.Message
	mu        sync.Mutex
	connectFn func(ctx context.Context) error
	sendFn    func(msg *mcp.Message) error
}

func NewMockTransport() *MockTransport {
	return &MockTransport{
		messages: make(chan *mcp.Message, 10),
		outbox:   make(chan *mcp.Message, 10),
	}
}

func (m *MockTransport) Connect(ctx context.Context) error {
	if m.connectFn != nil {
		return m.connectFn(ctx)
	}
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()
	return nil
}

func (m *MockTransport) Send(msg *mcp.Message) error {
	if m.sendFn != nil {
		return m.sendFn(msg)
	}
	select {
	case m.outbox <- msg:
		return nil
	default:
		return fmt.Errorf("outbox full")
	}
}

func (m *MockTransport) Receive() <-chan *mcp.Message {
	return m.messages
}

func (m *MockTransport) Close() error {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
	close(m.messages)
	close(m.outbox)
	return nil
}

func (m *MockTransport) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func TestNew(t *testing.T) {
	// Create test server for session creation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"sessionId": "test-session-123",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	
	cfg := config.DefaultConfig()
	cfg.Server.URL = server.URL
	cfg.Server.Transport = mcp.TransportHTTP // Use HTTP to avoid SSE implementation
	
	// This will fail because HTTP transport is not implemented
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("Expected not implemented error, got %v", err)
	}
}

func TestBridge_Close(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/session") {
			if r.Method == "DELETE" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	
	bridge := &Bridge{
		config: config.DefaultConfig(),
		session: &SessionManager{
			sessionID: "test-session",
			serverURL: server.URL,
		},
		stdio:  nil,
		remote: NewMockTransport(),
	}
	
	err := bridge.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestMessageRouter_RegisterPending(t *testing.T) {
	router := &MessageRouter{}
	
	ch := make(chan *mcp.Message, 1)
	router.RegisterPending("test-id", ch)
	
	// Verify it was stored
	if val, ok := router.pending.Load("test-id"); !ok {
		t.Error("Pending request not registered")
	} else if val.(chan<- *mcp.Message) != ch {
		t.Error("Wrong channel stored")
	}
}

func TestMessageRouter_RemovePending(t *testing.T) {
	router := &MessageRouter{}
	
	ch := make(chan *mcp.Message, 1)
	router.RegisterPending("test-id", ch)
	router.RemovePending("test-id")
	
	if _, ok := router.pending.Load("test-id"); ok {
		t.Error("Pending request not removed")
	}
}

func TestMessageRouter_Route(t *testing.T) {
	router := &MessageRouter{}
	
	// Test with nil ID
	msg := &mcp.Message{
		JSONRPC: "2.0",
	}
	router.Route(msg) // Should not panic
	
	// Test with valid ID and registered channel
	id := json.RawMessage(`"test-id"`)
	msg = &mcp.Message{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  json.RawMessage(`{"status":"ok"}`),
	}
	
	ch := make(chan *mcp.Message, 1)
	router.RegisterPending(`"test-id"`, ch)
	
	router.Route(msg)
	
	select {
	case received := <-ch:
		if received != msg {
			t.Error("Wrong message routed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Message not routed")
	}
	
	// Verify pending was removed
	if _, ok := router.pending.Load(`"test-id"`); ok {
		t.Error("Pending not removed after routing")
	}
	
	// Test with non-existent ID
	router.Route(msg) // Should not panic
	
	// Test with full channel
	ch2 := make(chan *mcp.Message) // Unbuffered
	router.RegisterPending("blocked", ch2)
	
	id2 := json.RawMessage(`"blocked"`)
	msg2 := &mcp.Message{ID: &id2}
	
	done := make(chan bool)
	go func() {
		router.Route(msg2)
		done <- true
	}()
	
	select {
	case <-done:
		// Good, didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("Route blocked on full channel")
	}
}

func TestSessionManager_UpdateActivity(t *testing.T) {
	session := &SessionManager{
		lastActivity: time.Now().Add(-time.Hour),
	}
	
	oldActivity := session.lastActivity
	session.UpdateActivity()
	
	if !session.lastActivity.After(oldActivity) {
		t.Error("Activity time not updated")
	}
}

func TestSessionManager_Close(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" && strings.Contains(r.URL.Path, "test-session") {
			called = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	
	session := &SessionManager{
		sessionID: "test-session",
		serverURL: server.URL,
	}
	
	err := session.Close(server.URL)
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
	
	if !called {
		t.Error("DELETE request not made")
	}
}

func TestCreateSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"sessionId": "new-session-456",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	
	session, err := createSession(server.URL)
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}
	
	if session.sessionID != "new-session-456" {
		t.Errorf("SessionID = %v, want new-session-456", session.sessionID)
	}
	
	if session.serverURL != server.URL {
		t.Errorf("ServerURL = %v, want %v", session.serverURL, server.URL)
	}
}

func TestCreateSession_Error(t *testing.T) {
	// Test server error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	
	_, err := createSession(server.URL)
	if err == nil {
		t.Error("Expected error for server error")
	}
	
	// Test invalid JSON
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server2.Close()
	
	_, err = createSession(server2.URL)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
	
	// Test network error
	_, err = createSession("http://invalid.local:99999")
	if err == nil {
		t.Error("Expected error for network error")
	}
}

func TestDetectTransport(t *testing.T) {
	// Test SSE detection
	sseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer sseServer.Close()
	
	transport := detectTransport(sseServer.URL)
	if transport != mcp.TransportSSE {
		t.Errorf("Expected SSE transport, got %v", transport)
	}
	
	// Test fallback to HTTP
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer httpServer.Close()
	
	transport = detectTransport(httpServer.URL)
	if transport != mcp.TransportHTTP {
		t.Errorf("Expected HTTP transport, got %v", transport)
	}
	
	// Test with invalid URL
	transport = detectTransport("http://invalid.local:99999")
	if transport != mcp.TransportHTTP {
		t.Errorf("Expected HTTP transport for invalid URL, got %v", transport)
	}
}

func TestBridge_HandleResponse(t *testing.T) {
	bridge := &Bridge{
		config: config.DefaultConfig(),
		router: &MessageRouter{},
		stdio:  transport.NewStdioHandler(),
	}
	
	// Test successful response
	respChan := make(chan *mcp.Message, 1)
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Result:  json.RawMessage(`{"status":"ok"}`),
	}
	respChan <- msg
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	done := make(chan bool)
	go func() {
		bridge.handleResponse(ctx, "test-id", respChan)
		done <- true
	}()
	
	select {
	case <-done:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Error("handleResponse didn't complete")
	}
	
	// Test timeout
	respChan2 := make(chan *mcp.Message, 1)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	
	go bridge.handleResponse(ctx2, "test-id-2", respChan2)
	time.Sleep(200 * time.Millisecond)
	// Should have timed out and cleaned up
	
	// Test context cancellation
	respChan3 := make(chan *mcp.Message, 1)
	ctx3, cancel3 := context.WithCancel(context.Background())
	
	go bridge.handleResponse(ctx3, "test-id-3", respChan3)
	cancel3()
	time.Sleep(50 * time.Millisecond)
	// Should have cancelled
}

// Benchmark tests
func BenchmarkMessageRouter_Route(b *testing.B) {
	router := &MessageRouter{}
	
	// Pre-register channels
	channels := make([]chan *mcp.Message, 100)
	for i := 0; i < 100; i++ {
		ch := make(chan *mcp.Message, 1)
		channels[i] = ch
		router.RegisterPending(fmt.Sprintf("id-%d", i), ch)
	}
	
	messages := make([]*mcp.Message, 100)
	for i := 0; i < 100; i++ {
		id := json.RawMessage(fmt.Sprintf(`"id-%d"`, i))
		messages[i] = &mcp.Message{
			ID: &id,
		}
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Re-register for next iteration
		idx := i % 100
		router.RegisterPending(fmt.Sprintf("id-%d", idx), channels[idx])
		router.Route(messages[idx])
		
		// Drain channel
		select {
		case <-channels[idx]:
		default:
		}
	}
}

func BenchmarkSessionManager_UpdateActivity(b *testing.B) {
	session := &SessionManager{}
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			session.UpdateActivity()
		}
	})
}