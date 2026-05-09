package ws

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/game"
)

// botThinkDelay is how long the bot "thinks" before acting. Long enough for
// humans to read the previous action; short enough that play stays snappy.
const botThinkDelay = 1200 * time.Millisecond

// BotAvatars / BotNames give bots distinct identities.
var (
	botAvatars = []string{"🤖", "👾", "🐙", "🦊", "🐯", "🦄", "🐸", "🐧"}
	botNames   = []string{"Bot Alpha", "Bot Beta", "Bot Gamma", "Bot Delta", "Bot Echo", "Bot Foxtrot", "Bot Golf", "Bot Hotel"}
	botSeq     uint64
	botSeqMu   sync.Mutex
)

// NewBotMeta returns a fresh PlayerMeta for a bot, with a random uid and a
// rotating display avatar/name. Caller fills in BuyIn before adding to room.
func NewBotMeta() PlayerMeta {
	botSeqMu.Lock()
	idx := int(botSeq) % len(botAvatars)
	botSeq++
	botSeqMu.Unlock()
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return PlayerMeta{
		UserID:   "bot:" + hex.EncodeToString(suffix),
		Nickname: botNames[idx],
		Avatar:   botAvatars[idx],
		IsBot:    true,
	}
}

// botPendingTurns guards against scheduling multiple goroutines for the same
// (room, seat) pair when state churn fires checkBotTurn repeatedly.
var (
	botPendingMu sync.Mutex
	botPending   = map[string]struct{}{}
)

func botPendingKey(roomID string, seat int) string {
	return roomID + ":" + boundedItoa(seat)
}

func boundedItoa(seat int) string {
	if seat < 0 {
		return "x"
	}
	if seat == 0 {
		return "0"
	}
	out := []byte{}
	for seat > 0 {
		out = append([]byte{byte('0' + seat%10)}, out...)
		seat /= 10
	}
	return string(out)
}

// checkBotTurn schedules a deferred bot action when the engine is currently
// waiting on a bot. Called after every event-emitting hub operation.
func (h *Hub) checkBotTurn(room *Room) {
	room.mu.RLock()
	eng := room.engine
	if eng == nil || eng.ActiveSeat < 0 {
		room.mu.RUnlock()
		return
	}
	if eng.Stage == game.StageWaiting || eng.Stage == game.StageHandComplete {
		room.mu.RUnlock()
		return
	}
	seat := eng.ActiveSeat
	botUID := ""
	for _, m := range room.players {
		if m.Seat == seat && m.IsBot {
			botUID = m.UserID
			break
		}
	}
	room.mu.RUnlock()
	if botUID == "" {
		return
	}
	key := botPendingKey(room.ID, seat)
	botPendingMu.Lock()
	if _, exists := botPending[key]; exists {
		botPendingMu.Unlock()
		return
	}
	botPending[key] = struct{}{}
	botPendingMu.Unlock()

	go func() {
		time.Sleep(botThinkDelay)
		botPendingMu.Lock()
		delete(botPending, key)
		botPendingMu.Unlock()
		h.botAct(room, botUID, seat)
	}()
}

// botAct decides + applies a bot's action. After applying, it propagates
// events and recursively checks whether the next-to-act is also a bot.
func (h *Hub) botAct(room *Room, uid string, expectedSeat int) {
	room.mu.RLock()
	eng := room.engine
	if eng == nil || eng.ActiveSeat != expectedSeat {
		room.mu.RUnlock()
		return
	}
	var ep *game.EnginePlayer
	for _, p := range eng.Players {
		if p.ID == uid {
			ep = p
			break
		}
	}
	if ep == nil || ep.State != game.PlayerActive {
		room.mu.RUnlock()
		return
	}
	action := decideBotAction(eng, ep)
	room.mu.RUnlock()

	events, err := room.ApplyAction(uid, action)
	if err != nil {
		log.Printf("[bot] %s apply %v err: %v", uid, action, err)
		return
	}
	h.broadcastEvents(room, events)
	h.advanceUntilBlocked(room)
	h.checkBotTurn(room)
}

// decideBotAction implements the trivial rules-based AI. Read-only on engine.
func decideBotAction(eng *game.Engine, p *game.EnginePlayer) game.Action {
	callAmount := eng.CurrentBet - p.BetThisRound
	if callAmount < 0 {
		callAmount = 0
	}
	canCheck := callAmount == 0
	myStack := p.Chips // current chips, post-bet-this-round excluded
	totalCommitable := myStack + p.BetThisRound

	// If we have no chips left we can only check (a no-op). Engine forbids
	// all-in with 0; check is the only legal move.
	if myStack <= 0 {
		if canCheck {
			return game.Action{Type: game.ActionCheck}
		}
		return game.Action{Type: game.ActionFold}
	}

	preflop := len(eng.Community) == 0
	var rank game.HandRank
	var preStrength int // 0..100, used pre-flop only
	if preflop {
		preStrength = preFlopScore(p.HoleCards)
	} else {
		cards := append([]game.Card{}, p.HoleCards...)
		cards = append(cards, eng.Community...)
		if len(cards) >= 5 {
			rank = game.EvaluateBest5(cards).Rank
		}
	}

	// Helpers
	tryRaise := func(target int) game.Action {
		// Cap target at our entire stack (turns into all-in if we shove).
		if target > totalCommitable {
			target = totalCommitable
		}
		// Must exceed currentBet AND meet min raise (or be all-in).
		raiseSize := target - eng.CurrentBet
		if raiseSize < eng.LastRaiseSize && target < totalCommitable {
			// Min-raise-bump: if we have the chips, raise to the min legal level.
			needed := eng.CurrentBet + eng.LastRaiseSize
			if needed <= totalCommitable {
				target = needed
			} else {
				return shoveOrCall(canCheck, callAmount, myStack)
			}
		}
		if target <= eng.CurrentBet {
			return shoveOrCall(canCheck, callAmount, myStack)
		}
		if target == totalCommitable {
			return game.Action{Type: game.ActionAllIn}
		}
		return game.Action{Type: game.ActionRaise, Amount: target}
	}
	callOrCheck := func() game.Action {
		if canCheck {
			return game.Action{Type: game.ActionCheck}
		}
		return game.Action{Type: game.ActionCall}
	}
	foldUnlessFree := func() game.Action {
		if canCheck {
			return game.Action{Type: game.ActionCheck}
		}
		return game.Action{Type: game.ActionFold}
	}

	if preflop {
		switch {
		case preStrength >= 90:
			// Premium — raise 3x BB.
			return tryRaise(eng.CurrentBet * 3)
		case preStrength >= 70:
			return tryRaise(eng.CurrentBet * 2)
		case preStrength >= 40:
			// Cheap call only.
			if callAmount <= eng.BigBlind {
				return callOrCheck()
			}
			return foldUnlessFree()
		default:
			return foldUnlessFree()
		}
	}

	// Post-flop heuristics by hand rank.
	switch {
	case rank >= game.FullHouse:
		return tryRaise(eng.CurrentBet*3 + eng.BigBlind*4)
	case rank >= game.Flush:
		return tryRaise(eng.CurrentBet*2 + eng.BigBlind*2)
	case rank == game.Straight, rank == game.ThreeOfAKind, rank == game.TwoPair:
		// Bet half pot if we can; call moderate raises.
		if canCheck {
			return tryRaise(eng.CurrentBet + eng.BigBlind*2)
		}
		if callAmount <= myStack/3 {
			return game.Action{Type: game.ActionCall}
		}
		return foldUnlessFree()
	case rank == game.OnePair:
		if canCheck {
			return game.Action{Type: game.ActionCheck}
		}
		if callAmount <= myStack/8 {
			return game.Action{Type: game.ActionCall}
		}
		return game.Action{Type: game.ActionFold}
	default:
		return foldUnlessFree()
	}
}

// preFlopScore returns a 0..100 strength heuristic for two hole cards.
// Premium pairs / AK / AQ score highest; trash hands score lowest.
func preFlopScore(hole []game.Card) int {
	if len(hole) != 2 {
		return 0
	}
	a, b := hole[0], hole[1]
	if a.Rank < b.Rank {
		a, b = b, a
	}
	pair := a.Rank == b.Rank
	suited := a.Suit == b.Suit
	hi, lo := int(a.Rank), int(b.Rank)
	switch {
	case pair && hi >= int(game.Queen):
		return 100 // QQ+
	case pair && hi >= int(game.Ten):
		return 90 // TT+
	case hi == int(game.Ace) && lo == int(game.King):
		return 95 // AK
	case hi == int(game.Ace) && lo == int(game.Queen):
		return 80
	case pair:
		return 60 // small pairs
	case hi == int(game.Ace) && suited:
		return 65 // suited Ax
	case suited && hi-lo == 1 && hi >= int(game.Ten):
		return 55 // high suited connectors
	case hi >= int(game.Jack) && lo >= int(game.Ten):
		return 50
	case suited && hi-lo == 1:
		return 40 // suited connectors
	default:
		return 20
	}
}

// shoveOrCall picks call or all-in when the raise validator would reject our
// preferred raise size. Used by tryRaise as a fallback so we never emit an
// invalid action.
func shoveOrCall(canCheck bool, callAmount, myStack int) game.Action {
	if canCheck {
		return game.Action{Type: game.ActionCheck}
	}
	if callAmount >= myStack {
		// Calling would be all-in by definition; the engine treats Call as
		// auto-all-in when chips < diff, so just call.
		return game.Action{Type: game.ActionCall}
	}
	return game.Action{Type: game.ActionCall}
}
