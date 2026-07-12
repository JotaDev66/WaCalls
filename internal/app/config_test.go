package app

import (
	"reflect"
	"testing"
)

func TestLoadConfigReadsEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("WACALLS_API_TOKEN", "tok")
	t.Setenv("WACALLS_CORS_ORIGINS", "https://a.example.com")
	t.Setenv("WACALLS_WEBRTC_UDP_PORT", "7881")
	t.Setenv("WACALLS_PUBLIC_IP", " 203.0.113.10 , , 198.51.100.7 ")

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
}

func TestLoadConfigDefaultsWhenUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("WACALLS_API_TOKEN", "")
	t.Setenv("WACALLS_CORS_ORIGINS", "")
	t.Setenv("WACALLS_WEBRTC_UDP_PORT", "")
	t.Setenv("WACALLS_PUBLIC_IP", "")

	cfg := LoadConfig(":8080", "wacalls.db", "", false, 8)
	if cfg.WebRTCUDPPort != 0 {
		t.Fatalf("WebRTCUDPPort = %d, want 0 when unset", cfg.WebRTCUDPPort)
	}
	if len(cfg.PublicIPs) != 0 {
		t.Fatalf("PublicIPs = %v, want empty", cfg.PublicIPs)
	}
}
