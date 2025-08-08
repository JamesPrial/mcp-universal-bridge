package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jamesprial/mcp-universal-bridge/internal/config"
	"github.com/jamesprial/mcp-universal-bridge/internal/transport"
	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

// Bridge connects stdio to remote MCP server
type Bridge struct {
	config      *config.Config
	stdio       *transport.StdioHandler
	remote      RemoteTransport
	router      *MessageRouter
	session     *SessionManager
	metrics     *Metrics
	wg          sync.WaitGroup
}

// RemoteTransport interface for different transport types
type RemoteTransport interface {
	Connect(ctx context.Context) error
	Send(msg *mcp.Message) error
	Receive() <-chan *mcp.Message
	Close() error
}

// MessageRouter handles message routing and correlation
type MessageRouter struct {
	pending sync.Map // map[id]chan<- *mcp.Message
}

// SessionManager manages session lifecycle
type SessionManager struct {
	sessionID   string
	serverURL   string
	created     time.Time
	lastActivity time.Time
	mu          sync.RWMutex
}

// New creates a new bridge
func New(cfg *config.Config) (*Bridge, error) {
	stdio := transport.NewStdioHandler()
	
	// Create session
	session, err := createSession(cfg.Server.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	
	// Detect and create appropriate transport
	transportType := cfg.Server.Transport
	if transportType == mcp.TransportAuto {
		transportType = detectTransport(cfg.Server.URL)
		log.Info().Str("transport", transportType.String()).Msg("Auto-detected transport")
	}
	
	var remote RemoteTransport
	switch transportType {
	case mcp.TransportSSE:
		remote = transport.NewSSEClient(cfg.Server.URL, session.sessionID)
	case mcp.TransportWebSocket:
		// remote = transport.NewWebSocketClient(cfg.Server.URL, session.sessionID)
		return nil, fmt.Errorf("WebSocket transport not yet implemented")
	case mcp.TransportHTTP:
		// remote = transport.NewHTTPClient(cfg.Server.URL, session.sessionID)
		return nil, fmt.Errorf("HTTP polling transport not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported transport: %s", transportType)
	}
	
	return &Bridge{
		config:  cfg,
		stdio:   stdio,
		remote:  remote,
		router:  &MessageRouter{},
		session: session,
		metrics: NewMetrics(),
	}, nil
}

// Start begins the bridge operation
func (b *Bridge) Start(ctx context.Context) error {
	// Start stdio handler
	if err := b.stdio.Start(ctx); err != nil {
		return fmt.Errorf("failed to start stdio: %w", err)
	}
	
	// Connect to remote
	if err := b.remote.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect remote: %w", err)
	}
	
	// Start routing goroutines
	b.wg.Add(2)
	go b.routeStdioToRemote(ctx)
	go b.routeRemoteToStdio(ctx)
	
	// Start metrics server if configured
	if b.config.Monitoring.MetricsPort > 0 {
		go b.metrics.Serve(b.config.Monitoring.MetricsPort)
	}
	
	log.Info().
		Str("server", b.config.Server.URL).
		Str("session", b.session.sessionID).
		Msg("Bridge started")
	
	// Wait for completion
	b.wg.Wait()
	
	return nil
}

func (b *Bridge) routeStdioToRemote(ctx context.Context) {
	defer b.wg.Done()
	
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-b.stdio.Receive():
			if msg == nil {
				continue
			}
			
			// Track metrics
			b.metrics.MessagesProcessed.WithLabelValues("stdin", "received").Inc()
			timer := prometheus.NewTimer(b.metrics.MessageLatency.WithLabelValues("stdin_to_remote"))
			
			// Register pending if it's a request
			if msg.IsRequest() && msg.ID != nil {
				respChan := make(chan *mcp.Message, 1)
				b.router.RegisterPending(string(*msg.ID), respChan)
				
				// Handle response correlation
				go b.handleResponse(ctx, string(*msg.ID), respChan)
			}
			
			// Send to remote
			if err := b.remote.Send(msg); err != nil {
				log.Error().Err(err).Msg("Failed to send to remote")
				b.metrics.MessagesProcessed.WithLabelValues("remote", "error").Inc()
			} else {
				b.metrics.MessagesProcessed.WithLabelValues("remote", "sent").Inc()
			}
			
			timer.ObserveDuration()
			b.session.UpdateActivity()
		}
	}
}

func (b *Bridge) routeRemoteToStdio(ctx context.Context) {
	defer b.wg.Done()
	
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-b.remote.Receive():
			if msg == nil {
				continue
			}
			
			// Track metrics
			b.metrics.MessagesProcessed.WithLabelValues("remote", "received").Inc()
			timer := prometheus.NewTimer(b.metrics.MessageLatency.WithLabelValues("remote_to_stdio"))
			
			// Check if this is a response to a pending request
			if msg.IsResponse() && msg.ID != nil {
				b.router.Route(msg)
			} else {
				// Forward to stdio
				if err := b.stdio.Send(msg); err != nil {
					log.Error().Err(err).Msg("Failed to send to stdio")
					b.metrics.MessagesProcessed.WithLabelValues("stdout", "error").Inc()
				} else {
					b.metrics.MessagesProcessed.WithLabelValues("stdout", "sent").Inc()
				}
			}
			
			timer.ObserveDuration()
			b.session.UpdateActivity()
		}
	}
}

func (b *Bridge) handleResponse(ctx context.Context, id string, respChan <-chan *mcp.Message) {
	select {
	case resp := <-respChan:
		// Forward response to stdio
		if err := b.stdio.Send(resp); err != nil {
			log.Error().Err(err).Str("id", id).Msg("Failed to send response to stdio")
		}
	case <-time.After(30 * time.Second):
		// Timeout - clean up
		b.router.RemovePending(id)
		log.Warn().Str("id", id).Msg("Request timeout")
	case <-ctx.Done():
		return
	}
}

// Close gracefully shuts down the bridge
func (b *Bridge) Close() error {
	log.Info().Msg("Shutting down bridge")
	
	// Close session
	if err := b.session.Close(b.config.Server.URL); err != nil {
		log.Error().Err(err).Msg("Failed to close session")
	}
	
	// Close transports
	if b.stdio != nil {
		b.stdio.Close()
	}
	if b.remote != nil {
		b.remote.Close()
	}
	
	return nil
}

// MessageRouter methods
func (r *MessageRouter) RegisterPending(id string, ch chan<- *mcp.Message) {
	r.pending.Store(id, ch)
}

func (r *MessageRouter) RemovePending(id string) {
	r.pending.Delete(id)
}

func (r *MessageRouter) Route(msg *mcp.Message) {
	if msg.ID == nil {
		return
	}
	
	idStr := string(*msg.ID)
	if ch, ok := r.pending.LoadAndDelete(idStr); ok {
		respChan := ch.(chan<- *mcp.Message)
		select {
		case respChan <- msg:
		default:
			log.Warn().Str("id", idStr).Msg("Response channel full")
		}
	}
}

// SessionManager methods
func (s *SessionManager) UpdateActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()
}

func (s *SessionManager) Close(serverURL string) error {
	url := fmt.Sprintf("%s/session/%s", serverURL, s.sessionID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	return nil
}

// Helper functions
func createSession(serverURL string) (*SessionManager, error) {
	url := fmt.Sprintf("%s/session", serverURL)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to create session: status %d", resp.StatusCode)
	}
	
	var result struct {
		SessionID string `json:"sessionId"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return &SessionManager{
		sessionID:    result.SessionID,
		serverURL:    serverURL,
		created:      time.Now(),
		lastActivity: time.Now(),
	}, nil
}

func detectTransport(serverURL string) mcp.TransportType {
	// Try SSE first
	url := fmt.Sprintf("%s/events", serverURL)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "text/event-stream")
	
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			contentType := resp.Header.Get("Content-Type")
			if strings.Contains(contentType, "event-stream") {
				return mcp.TransportSSE
			}
		}
	}
	
	// Default to HTTP
	return mcp.TransportHTTP
}