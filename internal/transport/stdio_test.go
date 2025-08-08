package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
)

// MockStdio provides mock stdin/stdout for testing
type MockStdio struct {
	input  *bytes.Buffer
	output *bytes.Buffer
	mu     sync.Mutex
}

func NewMockStdio() *MockStdio {
	return &MockStdio{
		input:  new(bytes.Buffer),
		output: new(bytes.Buffer),
	}
}

func (m *MockStdio) WriteInput(data string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.input.WriteString(data + "\n")
}

func (m *MockStdio) ReadOutput() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.output.String()
}

func TestNewStdioHandler(t *testing.T) {
	handler := NewStdioHandler()
	
	if handler == nil {
		t.Fatal("NewStdioHandler returned nil")
	}
	
	if handler.input == nil {
		t.Error("input scanner is nil")
	}
	
	if handler.output == nil {
		t.Error("output writer is nil")
	}
	
	if handler.inbox == nil {
		t.Error("inbox channel is nil")
	}
	
	if handler.outbox == nil {
		t.Error("outbox channel is nil")
	}
}

func TestStdioHandler_Send(t *testing.T) {
	handler := NewStdioHandler()
	
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
	}
	
	err := handler.Send(msg)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}
	
	// Fill the outbox to test buffer full error
	for i := 0; i < 100; i++ {
		_ = handler.Send(msg)
	}
	
	err = handler.Send(msg)
	if err == nil || !strings.Contains(err.Error(), "outbox full") {
		t.Error("Expected outbox full error")
	}
}

func TestStdioHandler_Receive(t *testing.T) {
	handler := NewStdioHandler()
	
	inbox := handler.Receive()
	if inbox == nil {
		t.Fatal("Receive() returned nil channel")
	}
	
	// Verify it's the same channel
	if inbox != handler.inbox {
		t.Error("Receive() returned different channel")
	}
}

func TestStdioHandler_Close(t *testing.T) {
	handler := NewStdioHandler()
	
	err := handler.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
	
	// Verify idempotency
	err = handler.Close()
	if err != nil {
		t.Errorf("Second Close() error = %v", err)
	}
	
	// Verify closed flag is set
	handler.mu.Lock()
	closed := handler.closed
	handler.mu.Unlock()
	
	if !closed {
		t.Error("Handler not marked as closed")
	}
}

func TestStdioHandler_ReadLoop(t *testing.T) {
	// Create a pipe for testing
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	
	handler := &StdioHandler{
		input:  bufio.NewScanner(reader),
		output: bufio.NewWriter(&bytes.Buffer{}),
		inbox:  make(chan *mcp.Message, 10),
		outbox: make(chan *mcp.Message, 10),
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start read loop
	go handler.readLoop(ctx)
	
	// Write valid message
	msg := `{"jsonrpc":"2.0","method":"test","params":null}`
	go func() {
		_, _ = writer.Write([]byte(msg + "\n"))
	}()
	
	// Wait for message
	select {
	case received := <-handler.inbox:
		if received.Method != "test" {
			t.Errorf("Expected method 'test', got %s", received.Method)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for message")
	}
	
	// Test invalid JSON
	go func() {
		_, _ = writer.Write([]byte("invalid json\n"))
	}()
	
	// Should not receive invalid message
	select {
	case <-handler.inbox:
		t.Error("Should not receive invalid message")
	case <-time.After(100 * time.Millisecond):
		// Expected timeout
	}
	
	// Test empty line
	go func() {
		_, _ = writer.Write([]byte("\n"))
	}()
	
	select {
	case <-handler.inbox:
		t.Error("Should not receive empty message")
	case <-time.After(100 * time.Millisecond):
		// Expected timeout
	}
	
	// Test context cancellation
	cancel()
	time.Sleep(100 * time.Millisecond)
	
	// Channel should be closed
	select {
	case _, ok := <-handler.inbox:
		if ok {
			t.Error("Channel should be closed after context cancel")
		}
	default:
		// Channel might not have items
	}
}

func TestStdioHandler_WriteLoop(t *testing.T) {
	output := &bytes.Buffer{}
	handler := &StdioHandler{
		input:  bufio.NewScanner(strings.NewReader("")),
		output: bufio.NewWriter(output),
		inbox:  make(chan *mcp.Message, 10),
		outbox: make(chan *mcp.Message, 10),
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start write loop
	go handler.writeLoop(ctx)
	
	// Send a message
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
	}
	
	handler.outbox <- msg
	time.Sleep(100 * time.Millisecond)
	
	// Check output
	written := output.String()
	if !strings.Contains(written, "test") {
		t.Errorf("Output doesn't contain expected method: %s", written)
	}
	
	if !strings.HasSuffix(written, "\n") {
		t.Error("Output should end with newline")
	}
	
	// Test nil message
	handler.outbox <- nil
	time.Sleep(100 * time.Millisecond)
	
	// Test closed handler
	handler.mu.Lock()
	handler.closed = true
	handler.mu.Unlock()
	
	handler.outbox <- msg
	time.Sleep(100 * time.Millisecond)
	
	// Test context cancellation
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestStdioHandler_Start(t *testing.T) {
	handler := NewStdioHandler()
	ctx := context.Background()
	
	err := handler.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}
}

func TestStdioHandler_ConcurrentAccess(t *testing.T) {
	handler := NewStdioHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	_ = handler.Start(ctx)
	
	var wg sync.WaitGroup
	
	// Concurrent sends
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			msg := &mcp.Message{
				JSONRPC: "2.0",
				Method:  "test",
				ID:      jsonRawMessage(id),
			}
			_ = handler.Send(msg)
		}(i)
	}
	
	// Concurrent receives
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inbox := handler.Receive()
			select {
			case <-inbox:
			case <-time.After(100 * time.Millisecond):
			}
		}()
	}
	
	// Wait for operations to complete before closing
	wg.Wait()
	
	// Single close at the end
	handler.Close()
}

func TestStdioHandler_BufferPool(t *testing.T) {
	handler := NewStdioHandler()
	
	// Get buffer from pool
	buf1 := handler.bufPool.Get().([]byte)
	if cap(buf1) != 4096 {
		t.Errorf("Expected buffer capacity 4096, got %d", cap(buf1))
	}
	
	// Put back and get again
	handler.bufPool.Put(buf1)
	buf2 := handler.bufPool.Get().([]byte)
	
	// Should reuse the same buffer
	if cap(buf2) != cap(buf1) {
		t.Error("Buffer pool not reusing buffers")
	}
}

func TestStdioHandler_LargeMessage(t *testing.T) {
	handler := NewStdioHandler()
	
	// Create large message
	largeData := make([]byte, 1024*1024) // 1MB
	for i := range largeData {
		largeData[i] = 'a'
	}
	
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
		Params:  json.RawMessage(largeData),
	}
	
	err := handler.Send(msg)
	if err != nil {
		t.Errorf("Failed to send large message: %v", err)
	}
}

// Benchmark tests
func BenchmarkStdioHandler_Send(b *testing.B) {
	handler := NewStdioHandler()
	ctx := context.Background()
	_ = handler.Start(ctx)
	
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
		Params:  json.RawMessage(`{"key":"value"}`),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.Send(msg)
	}
}

func BenchmarkStdioHandler_Concurrent(b *testing.B) {
	handler := NewStdioHandler()
	ctx := context.Background()
	_ = handler.Start(ctx)
	
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
	}
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = handler.Send(msg)
		}
	})
}

// Helper to create json.RawMessage from int
func jsonRawMessage(i int) *json.RawMessage {
	data, _ := json.Marshal(i)
	raw := json.RawMessage(data)
	return &raw
}