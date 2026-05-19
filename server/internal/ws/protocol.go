package ws

import (
	"encoding/json"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/game"
)

// Wire-format message envelopes. JSON-encoded over WebSocket text frames.

type ClientMsgType string

const (
	CMsgJoin   ClientMsgType = "join"
	CMsgLeave  ClientMsgType = "leave"
	CMsgAction ClientMsgType = "action"
	CMsgRebuy  ClientMsgType = "rebuy"
	CMsgChat   ClientMsgType = "chat"
	CMsgPing   ClientMsgType = "ping"
)

type ClientMessage struct {
	Type ClientMsgType   `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type JoinPayload struct {
	RoomID   string `json:"roomId"`
	UserID   string `json:"userId"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	BuyIn    int    `json:"buyIn"`
	Password string `json:"password,omitempty"`
}

type LeavePayload struct {
	RoomID string `json:"roomId"`
}

type ActionPayload struct {
	RoomID string `json:"roomId"`
	Type   string `json:"type"` // "fold" | "check" | "call" | "raise" | "all-in"
	Amount int    `json:"amount,omitempty"`
}

type RebuyPayload struct {
	RoomID string `json:"roomId"`
	Amount int    `json:"amount"`
}

// ChatPayload carries a single short emoji or short text reaction. Server
// caps the rendered length and rejects anything longer.
type ChatPayload struct {
	RoomID string `json:"roomId"`
	Emoji  string `json:"emoji"`
}

type ServerMsgType string

const (
	SMsgJoined       ServerMsgType = "joined"
	SMsgError        ServerMsgType = "error"
	SMsgRoomState    ServerMsgType = "room-state"
	SMsgGameEvent    ServerMsgType = "game-event"
	SMsgPlayerJoined ServerMsgType = "player-joined"
	SMsgPlayerLeft   ServerMsgType = "player-left"
	SMsgChat         ServerMsgType = "chat"
	SMsgPong         ServerMsgType = "pong"
	SMsgGameEnded    ServerMsgType = "game-ended"
)

type ServerMessage struct {
	Type ServerMsgType `json:"type"`
	Data any           `json:"data,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type JoinedPayload struct {
	RoomID string `json:"roomId"`
	Seat   int    `json:"seat"`
	UserID string `json:"userId"`
}

// PlayerView is a per-viewer projection of an EnginePlayer. Hole cards are
// only filled in when the viewer is the player themselves OR the hand is at
// showdown (where cards are revealed for non-folded players).
type PlayerView struct {
	UserID       string      `json:"userId"`
	Seat         int         `json:"seat"`
	Nickname     string      `json:"nickname"`
	Avatar       string      `json:"avatar"`
	Chips        int         `json:"chips"`
	BetThisRound int         `json:"betThisRound"`
	State        string      `json:"state"`
	HoleCards    []game.Card `json:"holeCards,omitempty"`
	HasActed     bool        `json:"hasActed"`
	IsDealer     bool        `json:"isDealer"`
	IsSmallBlind bool        `json:"isSmallBlind"`
	IsBigBlind   bool        `json:"isBigBlind"`
	RebuyCount   int         `json:"rebuyCount"`
}

// RoomStateView is the full snapshot a client receives. Built per-viewer.
type RoomStateView struct {
	RoomID          string       `json:"roomId"`
	Stage           string       `json:"stage"`
	Pot             int          `json:"pot"`
	CurrentBet      int          `json:"currentBet"`
	MinRaise        int          `json:"minRaise"`
	ActiveSeat      int          `json:"activeSeat"`
	DealerSeat      int          `json:"dealerSeat"`
	SmallBlind      int          `json:"smallBlind"`
	BigBlind        int          `json:"bigBlind"`
	MaxSeats        int          `json:"maxSeats"`
	Community       []game.Card  `json:"community"`
	RevealedCount   int          `json:"revealedCount"`
	Players         []PlayerView `json:"players"`
	ViewerSeat      int          `json:"viewerSeat"` // -1 if viewer is a spectator
	HasPassword     bool         `json:"hasPassword"`
	DurationMinutes int          `json:"durationMinutes"`
	EndsAt          int64        `json:"endsAt"` // unix ms; 0 if no limit
	EndPending      bool         `json:"endPending"`
	Ended           bool         `json:"ended"`
}

// GameEndedPayload is broadcast once when a duration-limited game finalises.
type GameEndedPayload struct {
	RoomID   string                  `json:"roomId"`
	EndedAt  int64                   `json:"endedAt"` // unix ms
	Players  []PlayerSettlementView  `json:"players"` // sorted by net desc
}

type PlayerSettlementView struct {
	UserID     string `json:"userId"`
	Nickname   string `json:"nickname"`
	Avatar     string `json:"avatar"`
	Seat       int    `json:"seat"`
	IsBot      bool   `json:"isBot"`
	Chips      int    `json:"chips"`
	TotalBuyIn int    `json:"totalBuyIn"`
	Net        int    `json:"net"`
	Rank       int    `json:"rank"`
}

// GameEventPayload wraps an engine.Event for transport.
type GameEventPayload struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}
