// Package api hosts non-WebSocket HTTP endpoints (login, room listing, health).
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/auth"
)

// WxConfig describes how to talk to the WeChat code2session endpoint.
// When AppID or AppSecret is empty, the login handler runs in DEV mode and
// trusts the client-supplied uid/nickname/avatar so local testing without a
// real AppID still works.
type WxConfig struct {
	AppID     string
	AppSecret string

	// Endpoint allows the test suite to point at a fake server. Empty value
	// uses the real https://api.weixin.qq.com endpoint.
	Endpoint string

	// HTTPClient is optional. Defaults to a 5s-timeout client.
	HTTPClient *http.Client
}

// LoginRequest is the JSON body POSTed to /login by the mini-program.
type LoginRequest struct {
	Code     string `json:"code"`     // wx.login code (prod)
	UserID   string `json:"userId"`   // dev mode fallback / first-time profile
	Nickname string `json:"nickname"` // optional display name
	Avatar   string `json:"avatar"`   // optional emoji
}

// LoginResponse is returned to the client on success.
type LoginResponse struct {
	Token     string `json:"token"`
	UserID    string `json:"userId"`
	Nickname  string `json:"nickname"`
	Avatar    string `json:"avatar"`
	ExpiresAt int64  `json:"expiresAt"` // unix seconds
}

// LoginHandler returns an http.Handler that mints a session token.
// In production (wx.AppID + AppSecret set) it exchanges the wx.login code for
// an openid via code2session. In DEV mode (either left empty) it trusts the
// uid the client supplied, which is what the existing "玩家XXXX" identity
// flow already produces.
func LoginHandler(signer *auth.Signer, wx WxConfig) http.Handler {
	devMode := wx.AppID == "" || wx.AppSecret == ""
	if wx.HTTPClient == nil {
		wx.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if wx.Endpoint == "" {
		wx.Endpoint = "https://api.weixin.qq.com/sns/jscode2session"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req LoginRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad-payload", err.Error())
			return
		}

		var uid string
		if devMode {
			uid = req.UserID
			if uid == "" {
				writeJSONError(w, http.StatusBadRequest, "missing-userid",
					"dev mode requires userId in payload")
				return
			}
		} else {
			openid, err := exchangeCode(wx, req.Code)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "wx-login-failed", err.Error())
				return
			}
			// Prefix to avoid colliding with dev-mode locally-generated ids
			uid = "wx:" + openid
		}

		nickname := req.Nickname
		if nickname == "" {
			nickname = "玩家" + lastN(uid, 4)
		}
		avatar := req.Avatar
		if avatar == "" {
			avatar = "😎"
		}

		tok, exp, err := signer.Issue(uid, nickname, avatar)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "issue-failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, LoginResponse{
			Token:     tok,
			UserID:    uid,
			Nickname:  nickname,
			Avatar:    avatar,
			ExpiresAt: exp.Unix(),
		})
	})
}

// exchangeCode calls WeChat's jscode2session and returns the openid.
func exchangeCode(wx WxConfig, code string) (string, error) {
	if code == "" {
		return "", errors.New("empty code")
	}
	q := url.Values{}
	q.Set("appid", wx.AppID)
	q.Set("secret", wx.AppSecret)
	q.Set("js_code", code)
	q.Set("grant_type", "authorization_code")
	resp, err := wx.HTTPClient.Get(wx.Endpoint + "?" + q.Encode())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}
	var parsed struct {
		OpenID  string `json:"openid"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode wx response: %w; body=%s", err, string(body))
	}
	if parsed.ErrCode != 0 {
		return "", fmt.Errorf("wx errcode=%d msg=%s", parsed.ErrCode, parsed.ErrMsg)
	}
	if parsed.OpenID == "" {
		return "", fmt.Errorf("missing openid in wx response: %s", string(body))
	}
	return parsed.OpenID, nil
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
