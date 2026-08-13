package config

import (
	"os"
	"testing"
)

func TestLoadConfig_FromYAML(t *testing.T) {
	yaml := `
http:
  listen: ":9090"
lmtp:
  listen: ":8024"
socket:
  path: "/tmp/xlistman.sock"
database:
  path: "./test.db"
smtp:
  host: "mail.example.com"
  port: 587
web:
  base_url: "https://lists.example.com"
rate_limits:
  subscribe_per_hour: 10
  magic_link_per_hour: 5
  posts_per_hour: 20
`
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes returned error: %v", err)
	}
	if cfg.HTTP.Listen != ":9090" {
		t.Errorf("HTTP.Listen = %q, want %q", cfg.HTTP.Listen, ":9090")
	}
	if cfg.SMTP.Host != "mail.example.com" {
		t.Errorf("SMTP.Host = %q, want %q", cfg.SMTP.Host, "mail.example.com")
	}
	if cfg.SMTP.Port != 587 {
		t.Errorf("SMTP.Port = %d, want %d", cfg.SMTP.Port, 587)
	}
	if cfg.Web.BaseURL != "https://lists.example.com" {
		t.Errorf("Web.BaseURL = %q, want %q", cfg.Web.BaseURL, "https://lists.example.com")
	}
	if cfg.RateLimits.SubscribePerHour != 10 {
		t.Errorf("RateLimits.SubscribePerHour = %d, want %d", cfg.RateLimits.SubscribePerHour, 10)
	}
}

func TestLoadConfig_AppliesDefaults(t *testing.T) {
	cfg, err := LoadFromBytes([]byte("{}"))
	if err != nil {
		t.Fatalf("LoadFromBytes returned error: %v", err)
	}
	if cfg.HTTP.Listen != ":8080" {
		t.Errorf("default HTTP.Listen = %q, want %q", cfg.HTTP.Listen, ":8080")
	}
	if cfg.LMTP.Listen != ":8024" {
		t.Errorf("default LMTP.Listen = %q, want %q", cfg.LMTP.Listen, ":8024")
	}
	if cfg.Socket.Path != "/var/run/xlistman.sock" {
		t.Errorf("default Socket.Path = %q, want %q", cfg.Socket.Path, "/var/run/xlistman.sock")
	}
	if cfg.Database.Path != "./xlistman.db" {
		t.Errorf("default Database.Path = %q, want %q", cfg.Database.Path, "./xlistman.db")
	}
	if cfg.SMTP.Host != "localhost" {
		t.Errorf("default SMTP.Host = %q, want %q", cfg.SMTP.Host, "localhost")
	}
	if cfg.SMTP.Port != 25 {
		t.Errorf("default SMTP.Port = %d, want %d", cfg.SMTP.Port, 25)
	}
	if cfg.RateLimits.SubscribePerHour != 5 {
		t.Errorf("default RateLimits.SubscribePerHour = %d, want %d", cfg.RateLimits.SubscribePerHour, 5)
	}
	if cfg.RateLimits.MagicLinkPerHour != 3 {
		t.Errorf("default RateLimits.MagicLinkPerHour = %d, want %d", cfg.RateLimits.MagicLinkPerHour, 3)
	}
	if cfg.RateLimits.PostsPerHour != 10 {
		t.Errorf("default RateLimits.PostsPerHour = %d, want %d", cfg.RateLimits.PostsPerHour, 10)
	}
}

func TestLoadConfig_EnvVarOverrides(t *testing.T) {
	yaml := `
http:
  listen: ":8080"
smtp:
  host: "localhost"
  port: 25
`
	os.Setenv("XLISTMAN_HTTP_LISTEN", ":3000")
	os.Setenv("XLISTMAN_SMTP_HOST", "relay.example.com")
	os.Setenv("XLISTMAN_SMTP_PORT", "587")
	defer os.Unsetenv("XLISTMAN_HTTP_LISTEN")
	defer os.Unsetenv("XLISTMAN_SMTP_HOST")
	defer os.Unsetenv("XLISTMAN_SMTP_PORT")

	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes returned error: %v", err)
	}
	if cfg.HTTP.Listen != ":3000" {
		t.Errorf("HTTP.Listen = %q, want %q (env override)", cfg.HTTP.Listen, ":3000")
	}
	if cfg.SMTP.Host != "relay.example.com" {
		t.Errorf("SMTP.Host = %q, want %q (env override)", cfg.SMTP.Host, "relay.example.com")
	}
	if cfg.SMTP.Port != 587 {
		t.Errorf("SMTP.Port = %d, want %d (env override)", cfg.SMTP.Port, 587)
	}
}

func TestLoadConfig_SecretReference(t *testing.T) {
	os.Setenv("SMTP_PASSWORD", "s3cret")
	defer os.Unsetenv("SMTP_PASSWORD")

	yaml := `
smtp:
  host: "localhost"
  port: 25
  password: "${SMTP_PASSWORD}"
`
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes returned error: %v", err)
	}
	if cfg.SMTP.Password != "s3cret" {
		t.Errorf("SMTP.Password = %q, want %q (secret reference)", cfg.SMTP.Password, "s3cret")
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	content := []byte(`
http:
  listen: ":7777"
smtp:
  host: "localhost"
  port: 25
`)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile returned error: %v", err)
	}
	if cfg.HTTP.Listen != ":7777" {
		t.Errorf("HTTP.Listen = %q, want %q", cfg.HTTP.Listen, ":7777")
	}
}

func TestGenerateDefaultConfig(t *testing.T) {
	yaml, err := GenerateDefault()
	if err != nil {
		t.Fatalf("GenerateDefault returned error: %v", err)
	}
	cfg, err := LoadFromBytes(yaml)
	if err != nil {
		t.Fatalf("Loading generated config returned error: %v", err)
	}
	if cfg.HTTP.Listen != ":8080" {
		t.Errorf("generated HTTP.Listen = %q, want %q", cfg.HTTP.Listen, ":8080")
	}
}
