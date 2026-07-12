package app

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr          string
	DBPath        string
	StaticDir     string
	Debug         bool
	MaxCalls      int
	DatabaseURL   string
	APIToken      string
	CORSOrigins   string
	WebRTCUDPPort int
	PublicIPs     []string
}

func LoadConfig(addr, dbPath, staticDir string, debug bool, maxCalls int) Config {
	return Config{
		Addr:          addr,
		DBPath:        dbPath,
		StaticDir:     staticDir,
		Debug:         debug,
		MaxCalls:      maxCalls,
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		APIToken:      os.Getenv("WACALLS_API_TOKEN"),
		CORSOrigins:   os.Getenv("WACALLS_CORS_ORIGINS"),
		WebRTCUDPPort: parseUDPPort(os.Getenv("WACALLS_WEBRTC_UDP_PORT")),
		PublicIPs:     parsePublicIPs(os.Getenv("WACALLS_PUBLIC_IP")),
	}
}

func parseUDPPort(raw string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(raw))
	return n
}

func parsePublicIPs(raw string) []string {
	var out []string
	for p := range strings.SplitSeq(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
