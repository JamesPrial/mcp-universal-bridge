package transport

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// SSEClient handles Server-Sent Events communication
type SSEClient struct {
	url          string
	sessionID    string
	client       *http.Client
	events       chan *mcp.Message
	outbox       chan *mcp.Message
	reconnector  *ExponentialBackoff
	circuitBreaker *CircuitBreaker
	rateLimiter  *rate.Limiter
	mu           sync.RWMutex
	connected    bool
	cancel       context.CancelFunc
}

// ExponentialBackoff implements exponential backoff with jitter
type ExponentialBackoff struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	attempt    int
	mu         sync.Mutex
}

// CircuitBreaker prevents cascade failures
type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration
	failures     int
	lastFailTime time.Time
	state        int // 0=closed, 1=open, 2=half-open
	mu           sync.Mutex
}

// NewSSEClient creates a new SSE client
func NewSSEClient(serverURL, sessionID string) *SSEClient {
	return &SSEClient{
		url:       serverURL,
		sessionID: sessionID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		events:      make(chan *mcp.Message, 100),
		outbox:      make(chan *mcp.Message, 100),
		reconnector: &ExponentialBackoff{
			Initial:    100 * time.Millisecond,
			Max:        30 * time.Second,
			Multiplier: 2,
		},
		circuitBreaker: &CircuitBreaker{
			maxFailures:  5,
			resetTimeout: 60 * time.Second,
		},
		rateLimiter: rate.NewLimiter(rate.Every(time.Millisecond), 100), // 1000 req/s burst of 100
	}
}

// Connect establishes SSE connection
func (c *SSEClient) Connect(ctx context.Context) error {
	return c.circuitBreaker.Call(func() error {
		return c.connectWithRetry(ctx)
	})
}

func (c *SSEClient) connectWithRetry(ctx context.Context) error {
	for {
		err := c.doConnect(ctx)
		if err == nil {
			c.reconnector.Reset()
			return nil
		}
		
		delay := c.reconnector.NextDelay()
		log.Error().Err(err).Dur("retry_in", delay).Msg("SSE connection failed, retrying")
		
		select {
		case <-time.After(delay):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *SSEClient) doConnect(ctx context.Context) error {
	url := fmt.Sprintf("%s/session/%s/events", c.url, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()
	
	log.Info().Str("url", url).Msg("SSE connected")
	
	// Start reading events
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	
	go c.readEvents(ctx, resp.Body)
	go c.sendLoop(ctx)
	
	return nil
}

func (c *SSEClient) readEvents(ctx context.Context, body io.ReadCloser) {
	defer body.Close()
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
	}()
	
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 65536), 10*1024*1024)
	
	var eventData []byte
	
	for scanner.Scan() {
		line := scanner.Bytes()
		
		// Empty line signals end of event
		if len(line) == 0 && len(eventData) > 0 {
			c.processEvent(eventData)
			eventData = nil
			continue
		}
		
		// Parse SSE format
		if bytes.HasPrefix(line, []byte("data: ")) {
			eventData = append(eventData, line[6:]...)
		} else if bytes.HasPrefix(line, []byte(":")) {
			// Comment/keep-alive
			continue
		}
	}
	
	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("SSE read error")
	}
}

func (c *SSEClient) processEvent(data []byte) {
	msg, err := mcp.ParseMessage(data)
	if err != nil {
		log.Error().Err(err).Str("data", string(data)).Msg("Failed to parse SSE event")
		return
	}
	
	select {
	case c.events <- msg:
		log.Debug().Str("method", msg.Method).Msg("SSE event received")
	default:
		log.Warn().Msg("Event channel full, dropping message")
	}
}

func (c *SSEClient) sendLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.outbox:
			if msg == nil {
				continue
			}
			
			// Rate limit
			if err := c.rateLimiter.Wait(ctx); err != nil {
				log.Error().Err(err).Msg("Rate limiter error")
				continue
			}
			
			if err := c.sendMessage(ctx, msg); err != nil {
				log.Error().Err(err).Msg("Failed to send message")
			}
		}
	}
}

func (c *SSEClient) sendMessage(ctx context.Context, msg *mcp.Message) error {
	url := fmt.Sprintf("%s/session/%s/message", c.url, c.sessionID)
	
	data, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	
	return nil
}

// Send queues a message for sending
func (c *SSEClient) Send(msg *mcp.Message) error {
	select {
	case c.outbox <- msg:
		return nil
	default:
		return fmt.Errorf("outbox full")
	}
}

// Receive returns the events channel
func (c *SSEClient) Receive() <-chan *mcp.Message {
	return c.events
}

// IsConnected returns the connection status
func (c *SSEClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Close closes the SSE connection
func (c *SSEClient) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	close(c.outbox)
	close(c.events)
	return nil
}

// NextDelay calculates the next backoff delay with jitter
func (eb *ExponentialBackoff) NextDelay() time.Duration {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	
	delay := float64(eb.Initial)
	for i := 0; i < eb.attempt; i++ {
		delay *= eb.Multiplier
	}
	
	if delay > float64(eb.Max) {
		delay = float64(eb.Max)
	}
	
	// Add jitter (±25%)
	jitter := delay * 0.25 * (2*randFloat() - 1)
	eb.attempt++
	
	return time.Duration(delay + jitter)
}

// Reset resets the backoff counter
func (eb *ExponentialBackoff) Reset() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.attempt = 0
}

// Call executes a function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	// Check if circuit is open
	if cb.state == 1 {
		if time.Since(cb.lastFailTime) > cb.resetTimeout {
			cb.state = 2 // half-open
			cb.failures = 0
		} else {
			return fmt.Errorf("circuit breaker open")
		}
	}
	
	err := fn()
	if err != nil {
		cb.failures++
		cb.lastFailTime = time.Now()
		
		if cb.failures >= cb.maxFailures {
			cb.state = 1 // open
			log.Warn().Int("failures", cb.failures).Msg("Circuit breaker opened")
		}
		return err
	}
	
	// Success - reset state
	if cb.state == 2 {
		cb.state = 0 // closed
		log.Info().Msg("Circuit breaker closed")
	}
	cb.failures = 0
	
	return nil
}

// Simple random float for jitter
func randFloat() float64 {
	return float64(time.Now().UnixNano()%1000) / 1000.0
}