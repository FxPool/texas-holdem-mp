package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/ws"
)

func TestRoomsListEmpty(t *testing.T) {
	hub := ws.NewHub(ws.RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	rec := httptest.NewRecorder()
	RoomsHandler(hub).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rooms", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestRoomsCreateAutoID(t *testing.T) {
	hub := ws.NewHub(ws.RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	body, _ := json.Marshal(CreateRoomRequest{SmallBlind: 100, BigBlind: 200})
	req := httptest.NewRequest(http.MethodPost, "/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RoomsHandler(hub).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateRoomResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.ID) != 4 {
		t.Errorf("ID should be 4 chars, got %q", resp.ID)
	}
	if resp.SmallBlind != 100 || resp.BigBlind != 200 {
		t.Errorf("blinds not honored: %+v", resp)
	}
	if hub.GetRoom(resp.ID) == nil {
		t.Errorf("room %s not registered", resp.ID)
	}
}

func TestRoomsCreateRejectsInvalidBlinds(t *testing.T) {
	hub := ws.NewHub(ws.RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	body, _ := json.Marshal(CreateRoomRequest{SmallBlind: 200, BigBlind: 100}) // bb < sb
	req := httptest.NewRequest(http.MethodPost, "/rooms", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	RoomsHandler(hub).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRoomsCreateRejectsDuplicateID(t *testing.T) {
	hub := ws.NewHub(ws.RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.GetOrCreateRoom("dup1")
	body, _ := json.Marshal(CreateRoomRequest{ID: "dup1"})
	req := httptest.NewRequest(http.MethodPost, "/rooms", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	RoomsHandler(hub).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
