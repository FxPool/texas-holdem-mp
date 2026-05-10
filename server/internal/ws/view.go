package ws

import (
	"github.com/jiangminghong/texas-holdem-mp/server/internal/game"
)

// BuildRoomStateView projects a Room's authoritative state into a per-viewer
// snapshot. Opponents' hole cards are hidden unless the hand is at showdown
// and the opponent has not folded.
//
// viewerUserID may be empty for spectators; in that case all hole cards are hidden.
// If no hand is in progress, returns a "Waiting" view listing seated players.
func BuildRoomStateView(r *Room, viewerUserID string) RoomStateView {
	r.mu.RLock()
	defer r.mu.RUnlock()

	endsAt := int64(0)
	if !r.EndsAt.IsZero() {
		endsAt = r.EndsAt.UnixMilli()
	}
	view := RoomStateView{
		RoomID:          r.ID,
		SmallBlind:      r.Config.SmallBlind,
		BigBlind:        r.Config.BigBlind,
		ActiveSeat:      -1,
		DealerSeat:      -1,
		ViewerSeat:      -1,
		HasPassword:     r.Config.Password != "",
		DurationMinutes: r.Config.DurationMinutes,
		EndsAt:          endsAt,
		EndPending:      r.EndPending,
		Ended:           r.Ended,
	}

	if r.engine == nil {
		view.Stage = "waiting"
		for _, m := range r.players {
			if m.UserID == viewerUserID {
				view.ViewerSeat = m.Seat
			}
			view.Players = append(view.Players, PlayerView{
				UserID:   m.UserID,
				Seat:     m.Seat,
				Nickname: m.Nickname,
				Avatar:   m.Avatar,
				Chips:    m.BuyIn,
				State:    "waiting",
			})
		}
		return view
	}

	view.Stage = r.engine.Stage.Slug()
	view.Pot = r.engine.Pot
	view.CurrentBet = r.engine.CurrentBet
	view.MinRaise = r.engine.LastRaiseSize
	view.ActiveSeat = r.engine.ActiveSeat
	view.DealerSeat = r.engine.DealerSeat
	view.Community = append([]game.Card{}, r.engine.Community...)
	view.RevealedCount = len(r.engine.Community)

	atShowdown := r.engine.Stage == game.StageShowdown || r.engine.Stage == game.StageHandComplete
	sbSeat := r.sbSeat
	bbSeat := r.bbSeat

	for _, p := range r.engine.Players {
		meta := r.players[p.ID]
		isMe := p.ID == viewerUserID
		if isMe {
			view.ViewerSeat = p.Seat
		}
		var holeCards []game.Card
		if isMe || (atShowdown && p.State != game.PlayerFolded && p.State != game.PlayerSitOut) {
			holeCards = append([]game.Card{}, p.HoleCards...)
		}
		nickname, avatar := "", ""
		if meta != nil {
			nickname = meta.Nickname
			avatar = meta.Avatar
		}
		view.Players = append(view.Players, PlayerView{
			UserID:       p.ID,
			Seat:         p.Seat,
			Nickname:     nickname,
			Avatar:       avatar,
			Chips:        p.Chips,
			BetThisRound: p.BetThisRound,
			State:        p.State.Slug(),
			HoleCards:    holeCards,
			HasActed:     p.HasActed,
			IsDealer:     p.Seat == r.engine.DealerSeat,
			IsSmallBlind: p.Seat == sbSeat,
			IsBigBlind:   p.Seat == bbSeat,
		})
	}
	return view
}
