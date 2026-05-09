package api

import (
	"net/http"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/ws"
)

// StatsHandler returns GET /stats — the leaderboard of all known users
// sorted by lifetime net chips. Returns [] when persistence is disabled.
//
// GET /stats?userId=<uid> filters down to one user.
func StatsHandler(store *ws.SnapshotStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if store == nil {
			writeJSON(w, http.StatusOK, []ws.UserStatsSnap{})
			return
		}
		if uid := r.URL.Query().Get("userId"); uid != "" {
			if s := store.GetUserStats(uid); s != nil {
				writeJSON(w, http.StatusOK, s)
				return
			}
			writeJSON(w, http.StatusOK, ws.UserStatsSnap{UserID: uid})
			return
		}
		writeJSON(w, http.StatusOK, store.GetStats())
	})
}
