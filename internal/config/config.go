package config

import (
	"fmt"
	"os"
	"time"

	"github.com/jamesprial/mcp-universal-bridge/pkg/mcp"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config represents the bridge configuration
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Performance PerformanceConfig `yaml:"performance"`
	Retry       RetryConfig       `yaml:"retry"`
	Monitoring  MonitoringConfig  `yaml:"monitoring"`
}

// ServerConfig contains server connection settings
type ServerConfig struct {
	URL       string            `yaml:"url"`
	Transport mcp.TransportType `yaml:"transport"`
	Auth      AuthConfig        `yaml:"auth,omitempty"`
}

// AuthConfig contains authentication settings
type AuthConfig struct {
	Type   string `yaml:"type,omitempty"`   // none, bearer, basic
	Token  string `yaml:"token,omitempty"`
	User   string `yaml:"user,omitempty"`
	Pass   string `yaml:"pass,omitempty"`
}

// PerformanceConfig contains performance tuning settings
type PerformanceConfig struct {
	BufferSize      int `yaml:"buffer_size"`
	MaxMessageSize  int `yaml:"max_message_size"`
	WorkerPoolSize  int `yaml:"worker_pool_size"`
	ConnectionPool  int `yaml:"connection_pool"`
}

// RetryConfig contains retry behavior settings
type RetryConfig struct {
	MaxAttempts   int           `yaml:"max_attempts"`
	InitialDelay  time.Duration `yaml:"initial_delay"`
	MaxDelay      time.Duration `yaml:"max_delay"`
	Multiplier    float64       `yaml:"multiplier"`
}

// MonitoringConfig contains monitoring settings
type MonitoringConfig struct {
	MetricsPort int    `yaml:"metrics_port"`
	HealthPort  int    `yaml:"health_port"`
	LogLevel    string `yaml:"log_level"`
	LogFormat   string `yaml:"log_format"` // json, console
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			URL:       "http://localhost:3000",
			Transport: mcp.TransportAuto,
		},
		Performance: PerformanceConfig{
			BufferSize:     1024,
			MaxMessageSize: 10 * 1024 * 1024, // 10MB
			WorkerPoolSize: 10,
			ConnectionPool: 5,
		},
		Retry: RetryConfig{
			MaxAttempts:  10,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     30 * time.Second,
			Multiplier:   2.0,
		},
		Monitoring: MonitoringConfig{
			MetricsPort: 0,    // disabled by default
			HealthPort:  0,    // disabled by default
			LogLevel:    "info",
			LogFormat:   "console",
		},
	}
}

// Load loads configuration from file and environment
func Load(configFile string, serverURL string) (*Config, error) {
	cfg := DefaultConfig()
	
	// Load from file if provided
	if configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		
		if err := viper.Unmarshal(cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}
	
	// Override with environment variables
	viper.SetEnvPrefix("MCP")
	viper.AutomaticEnv()
	
	// Override with command line arguments
	if serverURL != "" {
		cfg.Server.URL = serverURL
	}
	
	// Apply environment overrides
	if url := viper.GetString("SERVER_URL"); url != "" {
		cfg.Server.URL = url
	}
	
	if transport := viper.GetString("TRANSPORT"); transport != "" {
		switch transport {
		case "sse":
			cfg.Server.Transport = mcp.TransportSSE
		case "http":
			cfg.Server.Transport = mcp.TransportHTTP
		case "websocket", "ws":
			cfg.Server.Transport = mcp.TransportWebSocket
		default:
			cfg.Server.Transport = mcp.TransportAuto
		}
	}
	
	if level := viper.GetString("LOG_LEVEL"); level != "" {
		cfg.Monitoring.LogLevel = level
	}
	
	if port := viper.GetInt("METRICS_PORT"); port > 0 {
		cfg.Monitoring.MetricsPort = port
	}
	
	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	return cfg, nil
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Server.URL == "" {
		return fmt.Errorf("server URL is required")
	}
	
	if c.Performance.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be positive")
	}
	
	if c.Performance.MaxMessageSize <= 0 {
		return fmt.Errorf("max message size must be positive")
	}
	
	if c.Retry.MaxAttempts < 0 {
		return fmt.Errorf("max attempts must be non-negative")
	}
	
	if c.Retry.Multiplier <= 1 {
		return fmt.Errorf("retry multiplier must be greater than 1")
	}
	
	return nil
}

// String returns a string representation of the config
func (c *Config) String() string {
	data, _ := yaml.Marshal(c)
	return string(data)
}