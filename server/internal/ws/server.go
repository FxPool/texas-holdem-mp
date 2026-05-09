package ws

import (
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/auth"
)

var defaultUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Default: permit any origin. WeChat MiniProgram requests have no
		// Origin header at all, so blanket-rejecting non-empty Origin is
		// also OK in production. The handler may install a stricter
		// CheckOrigin via NewUpgrader.
		return true
	},
}

// NewUpgrader returns a websocket.Upgrader with origin policy controlled by
// allowedOrigins. An empty slice means "permissive" (the default). A non-empty
// slice means "only requests with no Origin header (mini-program native) or
// matching one of these allowed origins succeed".
func NewUpgrader(allowedOrigins []string) websocket.Upgrader {
	if len(allowedOrigins) == 0 {
		return defaultUpgrader
	}
	allow := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allow[o] = struct{}{}
	}
	return websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// No Origin header → mini-program WS or non-browser client.
				return true
			}
			_, ok := allow[origin]
			return ok
		},
	}
}

// HandlerOption configures the websocket HTTP handler. Today the only
// option is WithAuth which enables token-based authentication during the
// upgrade handshake.
type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	signer   *auth.Signer
	optional bool
	upgrader *websocket.Upgrader
}

// WithUpgrader replaces the default Upgrader. Use NewUpgrader(allowedOrigins)
// to build a stricter one.
func WithUpgrader(u websocket.Upgrader) HandlerOption {
	return func(c *handlerConfig) {
		uu := u
		c.upgrader = &uu
	}
}

// WithAuth enables required token authentication. The handler will reject
// the WebSocket upgrade unless the request carries a valid token in the
// `token` query parameter or in the `Authorization: Bearer <token>` header.
// The verified claims are attached to the resulting Conn as conn.Authenticated
// so the hub can trust the user identity instead of relying on the join
// payload supplied by the client.
func WithAuth(s *auth.Signer) HandlerOption {
	return func(c *handlerConfig) {
		c.signer = s
		c.optional = false
	}
}

// WithOptionalAuth verifies a token when one is supplied, attaching the
// resulting claims to the Conn. Connections without a token are still
// accepted (anonymous), but cannot benefit from impersonation protection.
// This is useful during the migration period before all clients learn to
// fetch a token from /login.
func WithOptionalAuth(s *auth.Signer) HandlerOption {
	return func(c *handlerConfig) {
		c.signer = s
		c.optional = true
	}
}

// HTTPHandler returns an http.Handler that upgrades to WebSocket and runs
// read/write loops bound to the given Hub. Pass WithAuth to require a
// signed session token on the request.
func HTTPHandler(hub *Hub, opts ...HandlerOption) http.Handler {
	cfg := &handlerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var claims *auth.Claims
		if cfg.signer != nil {
			tok := extractToken(r)
			if tok == "" {
				if !cfg.optional {
					http.Error(w, "missing token", http.StatusUnauthorized)
					return
				}
				// optional auth: accept without claims
			} else {
				c, err := cfg.signer.Verify(tok)
				if err != nil {
					http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
					return
				}
				claims = &c
			}
		}
		up := cfg.upgrader
		if up == nil {
			up = &defaultUpgrader
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, "upgrade failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		conn := NewConn(ws)
		conn.Authenticated = claims
		go conn.WriteLoop()
		conn.ReadLoop(func(c *Conn, msg ClientMessage) {
			hub.HandleClientMessage(c, msg)
		})
		hub.HandleDisconnect(conn)
	})
}

// extractToken pulls the auth token out of the standard places: the
// `token` query parameter (mini-program friendly) or an Authorization
// Bearer header.
func extractToken(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}
