package transport

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
	"github.com/rs/zerolog/log"
)

// StdioHandler handles stdin/stdout communication
type StdioHandler struct {
	input    *bufio.Scanner
	output   *bufio.Writer
	inbox    chan *mcp.Message
	outbox   chan *mcp.Message
	mu       sync.Mutex
	closed   bool
	bufPool  sync.Pool
}

// NewStdioHandler creates a new stdio handler
func NewStdioHandler() *StdioHandler {
	return &StdioHandler{
		input:  bufio.NewScanner(os.Stdin),
		output: bufio.NewWriter(os.Stdout),
		inbox:  make(chan *mcp.Message, 100),
		outbox: make(chan *mcp.Message, 100),
		bufPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 4096)
			},
		},
	}
}

// Start begins processing stdio
func (s *StdioHandler) Start(ctx context.Context) error {
	// Set max token size for scanner (10MB)
	s.input.Buffer(make([]byte, 0, 65536), 10*1024*1024)
	
	// Start reader goroutine
	go s.readLoop(ctx)
	
	// Start writer goroutine
	go s.writeLoop(ctx)
	
	log.Info().Msg("StdioHandler started")
	return nil
}

func (s *StdioHandler) readLoop(ctx context.Context) {
	defer close(s.inbox)
	
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if !s.input.Scan() {
				if err := s.input.Err(); err != nil {
					log.Error().Err(err).Msg("Error reading from stdin")
				}
				return
			}
			
			data := s.input.Bytes()
			if len(data) == 0 {
				continue
			}
			
			msg, err := mcp.ParseMessage(data)
			if err != nil {
				log.Error().Err(err).Str("data", string(data)).Msg("Failed to parse message")
				continue
			}
			
			select {
			case s.inbox <- msg:
				log.Debug().Str("method", msg.Method).Msg("Message received from stdin")
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *StdioHandler) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.outbox:
			if msg == nil {
				continue
			}
			
			data, err := msg.Marshal()
			if err != nil {
				log.Error().Err(err).Msg("Failed to marshal message")
				continue
			}
			
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				return
			}
			
			_, err = s.output.Write(data)
			if err == nil {
				err = s.output.WriteByte('\n')
			}
			if err == nil {
				err = s.output.Flush()
			}
			s.mu.Unlock()
			
			if err != nil {
				log.Error().Err(err).Msg("Failed to write to stdout")
				return
			}
			
			log.Debug().Str("method", msg.Method).Msg("Message sent to stdout")
		}
	}
}

// Send sends a message to stdout
func (s *StdioHandler) Send(msg *mcp.Message) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("handler closed")
	}
	s.mu.Unlock()
	
	select {
	case s.outbox <- msg:
		return nil
	default:
		return fmt.Errorf("outbox full")
	}
}

// Receive returns the inbox channel for receiving messages
func (s *StdioHandler) Receive() <-chan *mcp.Message {
	return s.inbox
}

// Close closes the handler
func (s *StdioHandler) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.closed {
		return nil
	}
	
	s.closed = true
	close(s.outbox)
	return s.output.Flush()
}