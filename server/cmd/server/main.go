package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/api"
	"github.com/jiangminghong/texas-holdem-mp/server/internal/auth"
	"github.com/jiangminghong/texas-holdem-mp/server/internal/ws"
)

func main() {
	hub := ws.NewHub(ws.RoomConfig{
		SmallBlind: 50,
		BigBlind:   100,
		MaxSeats:   9,
		MinPlayers: 2,
	})

	// Persistence (optional). Set STATE_FILE=/path/to/state.json to enable.
	// Without it, rooms + stats are in-memory and reset on each restart.
	statePath := os.Getenv("STATE_FILE")

	store := ws.NewSnapshotStore(statePath)
	if statePath != "" {
		if err := hub.AttachStore(store, envDuration("STATE_SAVE_INTERVAL", 30*time.Second)); err != nil {
			log.Fatalf("load snapshot: %v", err)
		}
		log.Printf("STATE_FILE=%s — persistent rooms + lifetime stats", statePath)
	}

	// Auth: load HMAC secret from env. In dev (unset), generate one and warn —
	// tokens won't survive a restart but local play will work.
	secretStr := os.Getenv("AUTH_SECRET")
	if secretStr == "" {
		gen, err := auth.GenerateSecret()
		if err != nil {
			log.Fatalf("generate dev secret: %v", err)
		}
		secretStr = gen
		log.Printf("AUTH_SECRET not set — using ephemeral dev secret (tokens reset on restart)")
	}
	signer := auth.NewSigner(auth.DecodeSecret(secretStr), envDuration("AUTH_TTL", auth.DefaultTTL))

	wxCfg := api.WxConfig{
		AppID:     os.Getenv("WX_APPID"),
		AppSecret: os.Getenv("WX_APPSECRET"),
	}
	if wxCfg.AppID == "" || wxCfg.AppSecret == "" {
		log.Printf("WX_APPID/WX_APPSECRET not set — /login runs in DEV mode (trusts client uid)")
	}

	// Auth on /ws is optional in dev to keep older clients working. Set
	// AUTH_REQUIRED=1 to enforce.
	wsOpts := []ws.HandlerOption{}
	if os.Getenv("AUTH_REQUIRED") == "1" {
		wsOpts = append(wsOpts, ws.WithAuth(signer))
		log.Printf("AUTH_REQUIRED=1 — /ws requires a signed session token")
	} else {
		// Soft auth: accept token if provided, but allow connections without one.
		// Implemented as a custom handler below.
		wsOpts = append(wsOpts, ws.WithOptionalAuth(signer))
	}

	// Origin whitelist: ALLOWED_ORIGINS=https://a.example.com,https://b.example.com
	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		list := strings.Split(origins, ",")
		clean := make([]string, 0, len(list))
		for _, o := range list {
			if t := strings.TrimSpace(o); t != "" {
				clean = append(clean, t)
			}
		}
		if len(clean) > 0 {
			wsOpts = append(wsOpts, ws.WithUpgrader(ws.NewUpgrader(clean)))
			log.Printf("ALLOWED_ORIGINS=%v — non-mini-program origins outside this list are rejected", clean)
		}
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("/rooms", api.RoomsHandler(hub))
	loginLimit := api.IPRateLimit(10, 1) // 10 burst, 1/s sustained per IP
	mux.Handle("/login", loginLimit(api.LoginHandler(signer, wxCfg)))
	mux.Handle("/stats", api.StatsHandler(store))
	mux.Handle("/ws", ws.HTTPHandler(hub, wsOpts...))

	addr := envAddr("ADDR", ":18080")
	log.Printf("texas-holdem server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func envAddr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	return def
}
