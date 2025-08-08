package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
	"golang.org/x/time/rate"
)

func TestNewSSEClient(t *testing.T) {
	client := NewSSEClient("http://localhost:3000", "test-session")
	
	if client == nil {
		t.Fatal("NewSSEClient returned nil")
	}
	
	if client.url != "http://localhost:3000" {
		t.Errorf("URL = %v, want http://localhost:3000", client.url)
	}
	
	if client.sessionID != "test-session" {
		t.Errorf("SessionID = %v, want test-session", client.sessionID)
	}
	
	if client.events == nil {
		t.Error("events channel is nil")
	}
	
	if client.outbox == nil {
		t.Error("outbox channel is nil")
	}
	
	if client.reconnector == nil {
		t.Error("reconnector is nil")
	}
	
	if client.circuitBreaker == nil {
		t.Error("circuit breaker is nil")
	}
}

func TestSSEClient_Send(t *testing.T) {
	client := NewSSEClient("http://localhost:3000", "test")
	
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
	}
	
	err := client.Send(msg)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}
	
	// Fill the outbox
	for i := 0; i < 100; i++ {
		client.Send(msg)
	}
	
	err = client.Send(msg)
	if err == nil || !strings.Contains(err.Error(), "outbox full") {
		t.Error("Expected outbox full error")
	}
}

func TestSSEClient_Receive(t *testing.T) {
	client := NewSSEClient("http://localhost:3000", "test")
	
	events := client.Receive()
	if events == nil {
		t.Fatal("Receive() returned nil channel")
	}
	
	if events != client.events {
		t.Error("Receive() returned different channel")
	}
}

func TestSSEClient_IsConnected(t *testing.T) {
	client := NewSSEClient("http://localhost:3000", "test")
	
	if client.IsConnected() {
		t.Error("Should not be connected initially")
	}
	
	client.mu.Lock()
	client.connected = true
	client.mu.Unlock()
	
	if !client.IsConnected() {
		t.Error("Should be connected after setting flag")
	}
}

func TestSSEClient_Close(t *testing.T) {
	client := NewSSEClient("http://localhost:3000", "test")
	
	err := client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestSSEClient_Connect(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			
			// Send test event
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"test\"}\n\n")
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	
	client := NewSSEClient(server.URL, "test-session")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	err := client.Connect(ctx)
	if err != nil {
		t.Errorf("Connect() error = %v", err)
	}
	
	// Wait for connection to be established
	connected := false
	for i := 0; i < 20; i++ {
		if client.IsConnected() {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	
	if !connected {
		t.Error("Should be connected")
	}
}

func TestSSEClient_ConnectFailure(t *testing.T) {
	// Use invalid URL
	client := NewSSEClient("http://invalid.local:99999", "test")
	client.reconnector.Max = 100 * time.Millisecond // Speed up test
	
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	
	err := client.Connect(ctx)
	if err == nil {
		t.Error("Expected connection error")
	}
}

func TestSSEClient_ProcessEvent(t *testing.T) {
	client := NewSSEClient("http://localhost:3000", "test")
	
	// Valid event
	data := []byte(`{"jsonrpc":"2.0","method":"test","params":null}`)
	client.processEvent(data)
	
	select {
	case msg := <-client.events:
		if msg.Method != "test" {
			t.Errorf("Method = %v, want test", msg.Method)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for event")
	}
	
	// Invalid JSON
	client.processEvent([]byte("invalid json"))
	
	select {
	case <-client.events:
		t.Error("Should not receive invalid event")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
	
	// Fill events channel
	for i := 0; i < 100; i++ {
		client.events <- &mcp.Message{}
	}
	
	// Should drop message when channel full
	client.processEvent(data)
}

func TestSSEClient_ReadEvents(t *testing.T) {
	// Create test server that sends SSE events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		
		// Send multiple events
		fmt.Fprintf(w, ": comment\n\n")
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"test1\"}\n\n")
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"test2\"}\n\n")
		w.(http.Flusher).Flush()
	}))
	defer server.Close()
	
	client := NewSSEClient(server.URL, "test")
	resp, _ := http.Get(server.URL)
	defer resp.Body.Close()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go client.readEvents(ctx, resp.Body)
	
	// Should receive events
	var received []string
	for i := 0; i < 2; i++ {
		select {
		case msg := <-client.events:
			received = append(received, msg.Method)
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for events")
		}
	}
	
	if len(received) != 2 {
		t.Errorf("Expected 2 events, got %d", len(received))
	}
}

func TestExponentialBackoff_NextDelay(t *testing.T) {
	backoff := &ExponentialBackoff{
		Initial:    100 * time.Millisecond,
		Max:        1 * time.Second,
		Multiplier: 2,
	}
	
	// First delay should be around initial
	delay1 := backoff.NextDelay()
	if delay1 < 75*time.Millisecond || delay1 > 125*time.Millisecond {
		t.Errorf("First delay out of range: %v", delay1)
	}
	
	// Second delay should be doubled (with jitter)
	delay2 := backoff.NextDelay()
	if delay2 < 150*time.Millisecond || delay2 > 250*time.Millisecond {
		t.Errorf("Second delay out of range: %v", delay2)
	}
	
	// Keep increasing until max
	for i := 0; i < 10; i++ {
		backoff.NextDelay()
	}
	
	// Should be capped at max
	delayMax := backoff.NextDelay()
	if delayMax > 1250*time.Millisecond { // Max + 25% jitter
		t.Errorf("Delay exceeds max: %v", delayMax)
	}
}

func TestExponentialBackoff_Reset(t *testing.T) {
	backoff := &ExponentialBackoff{
		Initial:    100 * time.Millisecond,
		Max:        1 * time.Second,
		Multiplier: 2,
	}
	
	// Increase attempts
	backoff.NextDelay()
	backoff.NextDelay()
	
	// Reset
	backoff.Reset()
	
	// Should be back to initial
	delay := backoff.NextDelay()
	if delay < 75*time.Millisecond || delay > 125*time.Millisecond {
		t.Errorf("Reset delay out of range: %v", delay)
	}
}

func TestCircuitBreaker_Call(t *testing.T) {
	cb := &CircuitBreaker{
		maxFailures:  3,
		resetTimeout: 100 * time.Millisecond,
	}
	
	successCount := 0
	failCount := 0
	
	// Successful calls
	err := cb.Call(func() error {
		successCount++
		return nil
	})
	if err != nil {
		t.Errorf("Successful call returned error: %v", err)
	}
	
	// Failing calls
	for i := 0; i < 3; i++ {
		err = cb.Call(func() error {
			failCount++
			return fmt.Errorf("test error")
		})
		if err == nil {
			t.Error("Failed call should return error")
		}
	}
	
	// Circuit should be open now
	err = cb.Call(func() error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "circuit breaker open") {
		t.Error("Circuit should be open")
	}
	
	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)
	
	// Circuit should be half-open, next call succeeds
	err = cb.Call(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Half-open circuit should allow call: %v", err)
	}
	
	if cb.state != 0 {
		t.Error("Circuit should be closed after successful half-open call")
	}
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	cb := &CircuitBreaker{
		maxFailures:  5,
		resetTimeout: 100 * time.Millisecond,
	}
	
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var failCount atomic.Int32
	
	// Concurrent calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := cb.Call(func() error {
				if id%2 == 0 {
					successCount.Add(1)
					return nil
				}
				failCount.Add(1)
				return fmt.Errorf("error")
			})
			_ = err
		}(i)
	}
	
	wg.Wait()
}

func TestSSEClient_SendMessage(t *testing.T) {
	messageReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/message") {
			messageReceived = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	
	client := NewSSEClient(server.URL, "test")
	ctx := context.Background()
	
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
	}
	
	err := client.sendMessage(ctx, msg)
	if err != nil {
		t.Errorf("sendMessage() error = %v", err)
	}
	
	if !messageReceived {
		t.Error("Message was not sent to server")
	}
}

func TestSSEClient_RateLimiter(t *testing.T) {
	client := NewSSEClient("http://localhost:3000", "test")
	// Set a lower burst to test rate limiting
	client.rateLimiter = rate.NewLimiter(rate.Every(10*time.Millisecond), 1) // 100 req/s, burst of 1
	
	start := time.Now()
	
	// Should be rate limited after first request
	for i := 0; i < 5; i++ {
		client.rateLimiter.Wait(context.Background())
	}
	
	elapsed := time.Since(start)
	// With rate of 100/s and burst of 1, 5 requests should take at least 40ms (4 intervals of 10ms)
	if elapsed < 30*time.Millisecond {
		t.Errorf("Rate limiter not working: elapsed %v", elapsed)
	}
}

// Benchmark tests
func BenchmarkSSEClient_Send(b *testing.B) {
	client := NewSSEClient("http://localhost:3000", "test")
	msg := &mcp.Message{
		JSONRPC: "2.0",
		Method:  "test",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.Send(msg)
	}
}

func BenchmarkExponentialBackoff(b *testing.B) {
	backoff := &ExponentialBackoff{
		Initial:    100 * time.Millisecond,
		Max:        30 * time.Second,
		Multiplier: 2,
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backoff.NextDelay()
		if i%10 == 0 {
			backoff.Reset()
		}
	}
}

func BenchmarkCircuitBreaker(b *testing.B) {
	cb := &CircuitBreaker{
		maxFailures:  5,
		resetTimeout: time.Second,
	}
	
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cb.Call(func() error {
				if i%10 == 0 {
					return fmt.Errorf("error")
				}
				return nil
			})
			i++
		}
	})
}