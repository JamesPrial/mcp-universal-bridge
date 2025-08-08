package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	
	if cfg.Server.URL != "http://localhost:3000" {
		t.Errorf("Expected default server URL to be http://localhost:3000, got %s", cfg.Server.URL)
	}
	
	if cfg.Server.Transport != mcp.TransportAuto {
		t.Errorf("Expected default transport to be auto")
	}
	
	if cfg.Performance.BufferSize != 1024 {
		t.Errorf("Expected default buffer size to be 1024, got %d", cfg.Performance.BufferSize)
	}
	
	if cfg.Retry.Multiplier != 2.0 {
		t.Errorf("Expected default multiplier to be 2.0, got %f", cfg.Retry.Multiplier)
	}
	
	if cfg.Monitoring.LogLevel != "info" {
		t.Errorf("Expected default log level to be info, got %s", cfg.Monitoring.LogLevel)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing server URL",
			config: &Config{
				Server: ServerConfig{URL: ""},
				Performance: PerformanceConfig{
					BufferSize:     1024,
					MaxMessageSize: 1024,
				},
				Retry: RetryConfig{
					MaxAttempts: 5,
					Multiplier:  2.0,
				},
			},
			wantErr: true,
			errMsg:  "server URL is required",
		},
		{
			name: "invalid buffer size",
			config: &Config{
				Server: ServerConfig{URL: "http://localhost"},
				Performance: PerformanceConfig{
					BufferSize:     0,
					MaxMessageSize: 1024,
				},
				Retry: RetryConfig{
					MaxAttempts: 5,
					Multiplier:  2.0,
				},
			},
			wantErr: true,
			errMsg:  "buffer size must be positive",
		},
		{
			name: "invalid max message size",
			config: &Config{
				Server: ServerConfig{URL: "http://localhost"},
				Performance: PerformanceConfig{
					BufferSize:     1024,
					MaxMessageSize: -1,
				},
				Retry: RetryConfig{
					MaxAttempts: 5,
					Multiplier:  2.0,
				},
			},
			wantErr: true,
			errMsg:  "max message size must be positive",
		},
		{
			name: "invalid retry multiplier",
			config: &Config{
				Server: ServerConfig{URL: "http://localhost"},
				Performance: PerformanceConfig{
					BufferSize:     1024,
					MaxMessageSize: 1024,
				},
				Retry: RetryConfig{
					MaxAttempts: 5,
					Multiplier:  0.5,
				},
			},
			wantErr: true,
			errMsg:  "retry multiplier must be greater than 1",
		},
		{
			name: "negative max attempts",
			config: &Config{
				Server: ServerConfig{URL: "http://localhost"},
				Performance: PerformanceConfig{
					BufferSize:     1024,
					MaxMessageSize: 1024,
				},
				Retry: RetryConfig{
					MaxAttempts: -1,
					Multiplier:  2.0,
				},
			},
			wantErr: true,
			errMsg:  "max attempts must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")
	
	configContent := `
server:
  url: "http://test-server:8080"
  transport: "sse"
  auth:
    type: "bearer"
    token: "test-token"

performance:
  buffer_size: 2048
  max_message_size: 20971520
  worker_pool_size: 20

retry:
  max_attempts: 15
  initial_delay: 200ms
  max_delay: 60s
  multiplier: 3.0

monitoring:
  metrics_port: 9090
  health_port: 8080
  log_level: "debug"
  log_format: "json"
`
	
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}
	
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	
	// Verify loaded values
	if cfg.Server.URL != "http://test-server:8080" {
		t.Errorf("Server URL = %v, want http://test-server:8080", cfg.Server.URL)
	}
	
	if cfg.Server.Auth.Type != "bearer" {
		t.Errorf("Auth type = %v, want bearer", cfg.Server.Auth.Type)
	}
	
	if cfg.Performance.BufferSize != 2048 {
		t.Errorf("Buffer size = %v, want 2048", cfg.Performance.BufferSize)
	}
	
	if cfg.Retry.Multiplier != 3.0 {
		t.Errorf("Multiplier = %v, want 3.0", cfg.Retry.Multiplier)
	}
	
	if cfg.Monitoring.LogLevel != "debug" {
		t.Errorf("Log level = %v, want debug", cfg.Monitoring.LogLevel)
	}
}

func TestLoadFromFile_InvalidFile(t *testing.T) {
	_, err := LoadFromFile("/non/existent/file.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid.yaml")
	
	err := os.WriteFile(configFile, []byte("invalid: yaml: content:"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	
	_, err = LoadFromFile(configFile)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestConfig_String(t *testing.T) {
	cfg := DefaultConfig()
	str := cfg.String()
	
	if len(str) == 0 {
		t.Error("String() returned empty string")
	}
	
	// Check that it contains expected fields
	expectedFields := []string{"server:", "performance:", "retry:", "monitoring:"}
	for _, field := range expectedFields {
		if !contains(str, field) {
			t.Errorf("String() missing field %s", field)
		}
	}
}

func TestLoad_WithEnvironment(t *testing.T) {
	// Set environment variables
	os.Setenv("MCP_SERVER_URL", "http://env-server:9000")
	os.Setenv("MCP_TRANSPORT", "sse")
	os.Setenv("MCP_LOG_LEVEL", "error")
	os.Setenv("MCP_METRICS_PORT", "9999")
	
	defer func() {
		os.Unsetenv("MCP_SERVER_URL")
		os.Unsetenv("MCP_TRANSPORT")
		os.Unsetenv("MCP_LOG_LEVEL")
		os.Unsetenv("MCP_METRICS_PORT")
	}()
	
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	
	if cfg.Server.URL != "http://env-server:9000" {
		t.Errorf("Server URL = %v, want http://env-server:9000", cfg.Server.URL)
	}
	
	if cfg.Server.Transport != mcp.TransportSSE {
		t.Errorf("Transport = %v, want SSE", cfg.Server.Transport)
	}
	
	if cfg.Monitoring.LogLevel != "error" {
		t.Errorf("Log level = %v, want error", cfg.Monitoring.LogLevel)
	}
	
	if cfg.Monitoring.MetricsPort != 9999 {
		t.Errorf("Metrics port = %v, want 9999", cfg.Monitoring.MetricsPort)
	}
}

func TestLoad_CommandLineOverride(t *testing.T) {
	// Command line argument should override everything
	cfg, err := Load("", "http://cli-server:7000")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	
	if cfg.Server.URL != "http://cli-server:7000" {
		t.Errorf("Server URL = %v, want http://cli-server:7000", cfg.Server.URL)
	}
}

func TestServerConfig_AuthTypes(t *testing.T) {
	tests := []struct {
		name string
		auth AuthConfig
	}{
		{
			name: "no auth",
			auth: AuthConfig{Type: "none"},
		},
		{
			name: "bearer token",
			auth: AuthConfig{
				Type:  "bearer",
				Token: "test-token-123",
			},
		},
		{
			name: "basic auth",
			auth: AuthConfig{
				Type: "basic",
				User: "testuser",
				Pass: "testpass",
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Server.Auth = tt.auth
			
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() failed for auth type %s: %v", tt.name, err)
			}
		})
	}
}

func TestRetryConfig_Durations(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
	
	if cfg.InitialDelay != 100*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 100ms", cfg.InitialDelay)
	}
	
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v, want 30s", cfg.MaxDelay)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != substr && 
		(s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || 
		contains(s[1:], substr)))
}