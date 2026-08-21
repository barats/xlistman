package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all xListman configuration.
type Config struct {
	HTTP       HTTPConfig      `yaml:"http"`
	LMTP       LMTPConfig      `yaml:"lmtp"`
	Socket     SocketConfig    `yaml:"socket"`
	Database   DatabaseConfig  `yaml:"database"`
	SMTP       SMTPConfig      `yaml:"smtp"`
	Web        WebConfig       `yaml:"web"`
	RateLimits RateLimitConfig `yaml:"rate_limits"`
	Queue      QueueConfig     `yaml:"queue"`
}

type HTTPConfig struct {
	Listen string `yaml:"listen"`
}

type LMTPConfig struct {
	Listen string `yaml:"listen"`
}

type SocketConfig struct {
	Path string `yaml:"path"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Mode     string `yaml:"mode"`     // "smtp" (default) or "sink" (write to disk for development)
	SinkDir  string `yaml:"sink_dir"` // directory for outbound mail when mode is "sink"
}

type WebConfig struct {
	BaseURL string `yaml:"base_url"`
	// SiteName is the instance name shown in page titles, the web UI
	// header/footer, and social tags. Defaults to "xListman".
	SiteName string `yaml:"site_name"`
}

type RateLimitConfig struct {
	SubscribePerHour int `yaml:"subscribe_per_hour"`
	MagicLinkPerHour int `yaml:"magic_link_per_hour"`
	// MagicLinkPerIPPerHour caps magic-link requests per client IP, a coarse
	// anti-flood ceiling distinct from the per-email send cap above (ADR 0023).
	MagicLinkPerIPPerHour int `yaml:"magic_link_per_ip_per_hour"`
	PostsPerHour          int `yaml:"posts_per_hour"`
}

// QueueConfig controls the outbound queue worker.
type QueueConfig struct {
	// MaxRetries is the number of delivery attempts before a message is
	// bounced (posts) or dropped (notifications).
	MaxRetries int `yaml:"max_retries"`
}

const envPrefix = "XLISTMAN_"

// LoadFromFile reads a YAML config file, applies env var overrides, and returns a Config.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	return LoadFromBytes(data)
}

// LoadFromBytes parses YAML config data, applies defaults, env var overrides,
// and ${ENV_VAR} secret expansion.
func LoadFromBytes(data []byte) (*Config, error) {
	expanded := expandSecretRefs(data)

	cfg := &Config{}
	decoder := yaml.NewDecoder(bytes.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		if err.Error() == "EOF" {
			cfg = &Config{}
		} else {
			return nil, fmt.Errorf("parse config YAML: %w", err)
		}
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	return cfg, nil
}

// GenerateDefault returns a YAML config with default values and explanatory comments.
func GenerateDefault() ([]byte, error) {
	cfg := &Config{}
	applyDefaults(cfg)

	// Marshal to YAML, then prepend a header comment.
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal default config: %w", err)
	}

	header := []byte("# xListman configuration file\n# Environment variables with XLISTMAN_ prefix override these values.\n# Use ${ENV_VAR} syntax for secrets.\n\n")
	return append(header, data...), nil
}

var secretRefRe = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

func expandSecretRefs(data []byte) []byte {
	return secretRefRe.ReplaceAllFunc(data, func(match []byte) []byte {
		name := secretRefRe.FindSubmatch(match)[1]
		if val, ok := os.LookupEnv(string(name)); ok {
			return []byte(val)
		}
		return match
	})
}

func applyDefaults(cfg *Config) {
	if cfg.HTTP.Listen == "" {
		cfg.HTTP.Listen = ":8080"
	}
	if cfg.LMTP.Listen == "" {
		cfg.LMTP.Listen = ":8024"
	}
	if cfg.Socket.Path == "" {
		cfg.Socket.Path = "/var/run/xlistman.sock"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./xlistman.db"
	}
	if cfg.SMTP.Host == "" {
		cfg.SMTP.Host = "localhost"
	}
	if cfg.SMTP.Port == 0 {
		cfg.SMTP.Port = 25
	}
	if cfg.SMTP.Mode == "" {
		cfg.SMTP.Mode = "smtp"
	}
	if cfg.SMTP.SinkDir == "" {
		cfg.SMTP.SinkDir = "./mail"
	}
	if cfg.Web.SiteName == "" {
		cfg.Web.SiteName = "xListman"
	}
	if cfg.RateLimits.SubscribePerHour == 0 {
		cfg.RateLimits.SubscribePerHour = 5
	}
	if cfg.RateLimits.MagicLinkPerHour == 0 {
		cfg.RateLimits.MagicLinkPerHour = 3
	}
	if cfg.RateLimits.MagicLinkPerIPPerHour == 0 {
		cfg.RateLimits.MagicLinkPerIPPerHour = 50
	}
	if cfg.RateLimits.PostsPerHour == 0 {
		cfg.RateLimits.PostsPerHour = 10
	}
	if cfg.Queue.MaxRetries == 0 {
		cfg.Queue.MaxRetries = 8
	}
}

func applyEnvOverrides(cfg *Config) {
	setStrFromEnv(envPrefix+"HTTP_LISTEN", &cfg.HTTP.Listen)
	setStrFromEnv(envPrefix+"LMTP_LISTEN", &cfg.LMTP.Listen)
	setStrFromEnv(envPrefix+"SOCKET_PATH", &cfg.Socket.Path)
	setStrFromEnv(envPrefix+"DATABASE_PATH", &cfg.Database.Path)
	setStrFromEnv(envPrefix+"SMTP_HOST", &cfg.SMTP.Host)
	setIntFromEnv(envPrefix+"SMTP_PORT", &cfg.SMTP.Port)
	setStrFromEnv(envPrefix+"SMTP_USERNAME", &cfg.SMTP.Username)
	setStrFromEnv(envPrefix+"SMTP_PASSWORD", &cfg.SMTP.Password)
	setStrFromEnv(envPrefix+"SMTP_MODE", &cfg.SMTP.Mode)
	setStrFromEnv(envPrefix+"SMTP_SINK_DIR", &cfg.SMTP.SinkDir)
	setStrFromEnv(envPrefix+"WEB_BASE_URL", &cfg.Web.BaseURL)
	setStrFromEnv(envPrefix+"WEB_SITE_NAME", &cfg.Web.SiteName)
	setIntFromEnv(envPrefix+"RATE_LIMITS_SUBSCRIBE_PER_HOUR", &cfg.RateLimits.SubscribePerHour)
	setIntFromEnv(envPrefix+"RATE_LIMITS_MAGIC_LINK_PER_HOUR", &cfg.RateLimits.MagicLinkPerHour)
	setIntFromEnv(envPrefix+"RATE_LIMITS_MAGIC_LINK_PER_IP_PER_HOUR", &cfg.RateLimits.MagicLinkPerIPPerHour)
	setIntFromEnv(envPrefix+"RATE_LIMITS_POSTS_PER_HOUR", &cfg.RateLimits.PostsPerHour)
	setIntFromEnv(envPrefix+"QUEUE_MAX_RETRIES", &cfg.Queue.MaxRetries)
}

func setStrFromEnv(name string, target *string) {
	if val, ok := os.LookupEnv(name); ok {
		*target = val
	}
}

func setIntFromEnv(name string, target *int) {
	if val, ok := os.LookupEnv(name); ok {
		if n, err := strconv.Atoi(val); err == nil {
			*target = n
		}
	}
}

// Validate checks that required fields are present.
func (c *Config) Validate() error {
	if c.Web.BaseURL == "" {
		return fmt.Errorf("web.base_url is required")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path is required")
	}
	if !strings.HasPrefix(c.Web.BaseURL, "http://") && !strings.HasPrefix(c.Web.BaseURL, "https://") {
		return fmt.Errorf("web.base_url must start with http:// or https://")
	}
	return nil
}
