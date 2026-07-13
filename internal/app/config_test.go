package app

import (
	"reflect"
	"testing"
)

func TestLoadConfigWebhookEnvs(t *testing.T) {
	t.Setenv("WACALLS_WEBHOOK_URL", " https://hooks.example/wacalls ")
	t.Setenv("WACALLS_WEBHOOK_SECRET", "s3cr3t")
	cfg := LoadConfig(":0", "db", "", false, 0)
	if cfg.WebhookURL != "https://hooks.example/wacalls" || cfg.WebhookSecret != "s3cr3t" {
		t.Fatalf("webhook envs not loaded: %+v", cfg)
	}
}

func TestLoadConfigTrustedProxies(t *testing.T) {
	t.Setenv("WACALLS_TRUSTED_PROXIES", "127.0.0.1, 10.0.0.0/8")
	cfg := LoadConfig(":0", "db", "", false, 0)
	if cfg.TrustedProxies != "127.0.0.1, 10.0.0.0/8" {
		t.Fatalf("raw env not threaded: %q", cfg.TrustedProxies)
	}
}

func TestValidateConfigWebhook(t *testing.T) {
	if err := validateConfig(Config{WebhookURL: "https://x", WebhookSecret: ""}); err == nil {
		t.Fatal("url without secret must fail the boot")
	}
	if err := validateConfig(Config{WebhookURL: "https://x", WebhookSecret: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(Config{}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigReadsEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("WACALLS_API_TOKEN", "tok")
	t.Setenv("WACALLS_CORS_ORIGINS", "https://a.example.com")
	t.Setenv("WACALLS_WEBRTC_UDP_PORT", "7881")
	t.Setenv("WACALLS_PUBLIC_IP", " 203.0.113.10 , , 198.51.100.7 ")
	t.Setenv("WACALLS_RATE_LIMIT", "12.5")

	cfg := LoadConfig(":9000", "x.db", "static", true, 5)

	if cfg.Addr != ":9000" || cfg.DBPath != "x.db" || cfg.StaticDir != "static" || !cfg.Debug || cfg.MaxCalls != 5 {
		t.Fatalf("flag fields not threaded: %+v", cfg)
	}
	if cfg.DatabaseURL != "postgres://x" || cfg.APIToken != "tok" || cfg.CORSOrigins != "https://a.example.com" {
		t.Fatalf("env strings not read: %+v", cfg)
	}
	if cfg.WebRTCUDPPort != 7881 {
		t.Fatalf("WebRTCUDPPort = %d, want 7881", cfg.WebRTCUDPPort)
	}
	if !reflect.DeepEqual(cfg.PublicIPs, []string{"203.0.113.10", "198.51.100.7"}) {
		t.Fatalf("PublicIPs = %v, want trimmed split", cfg.PublicIPs)
	}
	if cfg.RateLimit != 12.5 {
		t.Fatalf("RateLimit = %v, want 12.5", cfg.RateLimit)
	}
}

func TestLoadConfigDefaultsWhenUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("WACALLS_API_TOKEN", "")
	t.Setenv("WACALLS_CORS_ORIGINS", "")
	t.Setenv("WACALLS_WEBRTC_UDP_PORT", "")
	t.Setenv("WACALLS_PUBLIC_IP", "")
	t.Setenv("WACALLS_RATE_LIMIT", "")

	cfg := LoadConfig(":8080", "wacalls.db", "", false, 8)
	if cfg.WebRTCUDPPort != 0 {
		t.Fatalf("WebRTCUDPPort = %d, want 0 when unset", cfg.WebRTCUDPPort)
	}
	if len(cfg.PublicIPs) != 0 {
		t.Fatalf("PublicIPs = %v, want empty", cfg.PublicIPs)
	}
	if cfg.RateLimit != 20 {
		t.Fatalf("RateLimit = %v, want default 20 when unset", cfg.RateLimit)
	}
}

func TestParseRateLimit(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
	}{
		{"", 20},
		{"  ", 20},
		{"0", 0},
		{"-3", 0},
		{"7.5", 7.5},
		{"garbage", 20},
	}
	for _, c := range cases {
		if got := parseRateLimit(c.raw); got != c.want {
			t.Fatalf("parseRateLimit(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
