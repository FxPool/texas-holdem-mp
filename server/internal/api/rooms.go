package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/ws"
)

// CreateRoomRequest is the body of POST /rooms. All fields optional;
// blanks fall through to the hub's default config.
type CreateRoomRequest struct {
	ID              string `json:"id"`
	SmallBlind      int    `json:"smallBlind"`
	BigBlind        int    `json:"bigBlind"`
	MaxSeats        int    `json:"maxSeats"`
	Bots            int    `json:"bots"`     // optional: seed N AI players
	BotBuyIn        int    `json:"botBuyIn"` // optional: chip stack per bot, default 1000
	Password        string `json:"password"` // optional; empty = public room
	DurationMinutes int    `json:"durationMinutes"`
}

type CreateRoomResponse struct {
	ID              string `json:"id"`
	SmallBlind      int    `json:"smallBlind"`
	BigBlind        int    `json:"bigBlind"`
	MaxSeats        int    `json:"maxSeats"`
	Bots            int    `json:"bots"`
	HasPassword     bool   `json:"hasPassword"`
	DurationMinutes int    `json:"durationMinutes"`
	EndsAt          int64  `json:"endsAt"` // unix ms; 0 if no limit
}

// RoomsHandler returns a single handler that serves both GET (list) and
// POST (create). Older deployments can keep using a separate /rooms list-only
// handler if they prefer.
func RoomsHandler(hub *ws.Hub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, hub.ListRooms())
		case http.MethodPost:
			handleCreate(hub, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func handleCreate(hub *ws.Hub, w http.ResponseWriter, r *http.Request) {
	var req CreateRoomRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "bad-payload", err.Error())
		return
	}
	id := req.ID
	if id == "" {
		gen, err := newRoomID()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "id-gen-failed", err.Error())
			return
		}
		id = gen
	}
	room, err := hub.CreateRoom(id, ws.RoomConfig{
		SmallBlind:      req.SmallBlind,
		BigBlind:        req.BigBlind,
		MaxSeats:        req.MaxSeats,
		Password:        req.Password,
		DurationMinutes: req.DurationMinutes,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "create-failed", err.Error())
		return
	}
	addedBots := 0
	if req.Bots > 0 {
		buyIn := req.BotBuyIn
		if buyIn <= 0 {
			buyIn = 1000
		}
		// Cap at MaxSeats-1 so a human can still join.
		want := req.Bots
		if want > room.Config.MaxSeats-1 {
			want = room.Config.MaxSeats - 1
		}
		for i := 0; i < want; i++ {
			meta := ws.NewBotMeta()
			meta.BuyIn = buyIn
			if _, err := room.AddBot(meta); err != nil {
				break
			}
			addedBots++
		}
	}
	endsAt := int64(0)
	if !room.EndsAt.IsZero() {
		endsAt = room.EndsAt.UnixMilli()
	}
	writeJSON(w, http.StatusOK, CreateRoomResponse{
		ID:              room.ID,
		SmallBlind:      room.Config.SmallBlind,
		BigBlind:        room.Config.BigBlind,
		MaxSeats:        room.Config.MaxSeats,
		Bots:            addedBots,
		HasPassword:     room.HasPassword(),
		DurationMinutes: room.Config.DurationMinutes,
		EndsAt:          endsAt,
	})
}

// newRoomID returns a 4-digit string room ID that doesn't start with 0.
// Cryptographic randomness keeps IDs unguessable for short-lived rooms.
func newRoomID() (string, error) {
	first, err := rand.Int(rand.Reader, big.NewInt(9))
	if err != nil {
		return "", err
	}
	rest, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		return "", err
	}
	d1 := first.Int64() + 1
	r2 := rest.Int64()
	id := []byte{
		byte('0' + d1),
		byte('0' + (r2/100)%10),
		byte('0' + (r2/10)%10),
		byte('0' + r2%10),
	}
	return string(id), nil
}
