package game

import (
	"errors"
	"fmt"
	"sort"
)

// ============================================================
// Types
// ============================================================

type Stage int

const (
	StageWaiting Stage = iota
	StagePreflop
	StageFlop
	StageTurn
	StageRiver
	StageShowdown
	StageHandComplete
)

func (s Stage) String() string {
	switch s {
	case StageWaiting:
		return "Waiting"
	case StagePreflop:
		return "Preflop"
	case StageFlop:
		return "Flop"
	case StageTurn:
		return "Turn"
	case StageRiver:
		return "River"
	case StageShowdown:
		return "Showdown"
	case StageHandComplete:
		return "HandComplete"
	}
	return "?"
}

// Slug returns the JSON-friendly stage name (lower-kebab).
func (s Stage) Slug() string {
	switch s {
	case StageWaiting:
		return "waiting"
	case StagePreflop:
		return "preflop"
	case StageFlop:
		return "flop"
	case StageTurn:
		return "turn"
	case StageRiver:
		return "river"
	case StageShowdown:
		return "showdown"
	case StageHandComplete:
		return "hand-complete"
	}
	return "?"
}

type PlayerState int

const (
	PlayerSitOut PlayerState = iota
	PlayerActive
	PlayerFolded
	PlayerAllIn
)

func (s PlayerState) String() string {
	switch s {
	case PlayerSitOut:
		return "SitOut"
	case PlayerActive:
		return "Active"
	case PlayerFolded:
		return "Folded"
	case PlayerAllIn:
		return "AllIn"
	}
	return "?"
}

// Slug returns the JSON-friendly state name (lower-kebab).
func (s PlayerState) Slug() string {
	switch s {
	case PlayerSitOut:
		return "sit-out"
	case PlayerActive:
		return "active"
	case PlayerFolded:
		return "folded"
	case PlayerAllIn:
		return "all-in"
	}
	return "?"
}

type EnginePlayer struct {
	ID           string
	Seat         int
	Chips        int
	HoleCards    []Card
	State        PlayerState
	BetThisRound int
	Committed    int
	HasActed     bool
}

type ActionType int

const (
	ActionFold ActionType = iota
	ActionCheck
	ActionCall
	ActionRaise
	ActionAllIn
)

func (a ActionType) String() string {
	switch a {
	case ActionFold:
		return "Fold"
	case ActionCheck:
		return "Check"
	case ActionCall:
		return "Call"
	case ActionRaise:
		return "Raise"
	case ActionAllIn:
		return "AllIn"
	}
	return "?"
}

type Action struct {
	Type ActionType
	// Amount is used only for Raise: the new total bet level (not the delta).
	// E.g. CurrentBet=200, raise to 600 → Amount=600.
	Amount int
}

type EventType string

const (
	EventBlindPosted    EventType = "blind-posted"
	EventHoleDealt      EventType = "hole-dealt"
	EventAction         EventType = "action"
	EventStageStart     EventType = "stage-start"
	EventCommunityDealt EventType = "community-dealt"
	EventShowdown       EventType = "showdown"
	EventHandComplete   EventType = "hand-complete"
)

type Event struct {
	Type EventType
	Data map[string]any
}

type Engine struct {
	Players       []*EnginePlayer // sorted by Seat asc on construction
	Stage         Stage
	DealerSeat    int
	SmallBlind    int
	BigBlind      int
	Pot           int
	CurrentBet    int
	LastRaiseSize int
	ActiveSeat    int // -1 when betting round closed
	Community     []Card

	deck        *Deck
	handStarted bool
}

// ============================================================
// Construction
// ============================================================

func NewEngine(players []*EnginePlayer, dealerSeat, sb, bb int) (*Engine, error) {
	if len(players) < 2 {
		return nil, errors.New("need at least 2 players")
	}
	if sb <= 0 || bb <= 0 || bb < sb {
		return nil, fmt.Errorf("invalid blinds sb=%d bb=%d", sb, bb)
	}

	sorted := append([]*EnginePlayer(nil), players...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seat < sorted[j].Seat })

	dealerFound := false
	for _, p := range sorted {
		if p.Seat == dealerSeat {
			dealerFound = true
			break
		}
	}
	if !dealerFound {
		return nil, fmt.Errorf("dealer seat %d not in players", dealerSeat)
	}

	return &Engine{
		Players:    sorted,
		Stage:      StageWaiting,
		DealerSeat: dealerSeat,
		SmallBlind: sb,
		BigBlind:   bb,
		ActiveSeat: -1,
	}, nil
}

// ============================================================
// Start: post blinds, deal hole cards, set first preflop actor
// ============================================================

func (e *Engine) Start() ([]Event, error) {
	if e.handStarted {
		return nil, errors.New("hand already started")
	}
	e.handStarted = true

	for _, p := range e.Players {
		if p.Chips > 0 {
			p.State = PlayerActive
		} else {
			p.State = PlayerSitOut
		}
		p.HoleCards = nil
		p.BetThisRound = 0
		p.Committed = 0
		p.HasActed = false
	}

	if e.countActive() < 2 {
		return nil, errors.New("need at least 2 active players (with chips)")
	}

	var sbSeat, bbSeat int
	if e.countActive() == 2 {
		// heads-up: dealer = SB
		sbSeat = e.DealerSeat
		bbSeat = e.nextActiveSeat(e.DealerSeat)
	} else {
		sbSeat = e.nextActiveSeat(e.DealerSeat)
		bbSeat = e.nextActiveSeat(sbSeat)
	}

	var events []Event
	sbAmt := e.postForced(sbSeat, e.SmallBlind)
	events = append(events, Event{Type: EventBlindPosted, Data: map[string]any{
		"seat": sbSeat, "amount": sbAmt, "kind": "small",
	}})
	bbAmt := e.postForced(bbSeat, e.BigBlind)
	events = append(events, Event{Type: EventBlindPosted, Data: map[string]any{
		"seat": bbSeat, "amount": bbAmt, "kind": "big",
	}})
	e.CurrentBet = e.BigBlind
	e.LastRaiseSize = e.BigBlind

	// Deal hole cards: 2 rounds, one card each starting from SB
	e.deck = NewDeck()
	if err := e.deck.Shuffle(); err != nil {
		return events, err
	}
	for round := 0; round < 2; round++ {
		seat := sbSeat
		for i := 0; i < e.countInHand(); i++ {
			cards, err := e.deck.Deal(1)
			if err != nil {
				return events, err
			}
			e.playerBySeat(seat).HoleCards = append(e.playerBySeat(seat).HoleCards, cards[0])
			seat = e.nextSeatInHand(seat)
		}
	}
	events = append(events, Event{Type: EventHoleDealt, Data: map[string]any{"count": 2}})

	// Preflop first to act:
	// - 2-handed: SB (dealer) acts first
	// - 3+ handed: UTG = next active after BB
	e.Stage = StagePreflop
	if e.countActive() == 2 {
		e.ActiveSeat = sbSeat
	} else {
		e.ActiveSeat = e.nextActiveSeat(bbSeat)
	}
	events = append(events, Event{Type: EventStageStart, Data: map[string]any{
		"stage": e.Stage.String(), "first": e.ActiveSeat,
	}})

	return events, nil
}

// ============================================================
// Apply: process a player action
// ============================================================

func (e *Engine) Apply(seat int, action Action) ([]Event, error) {
	if e.Stage != StagePreflop && e.Stage != StageFlop &&
		e.Stage != StageTurn && e.Stage != StageRiver {
		return nil, fmt.Errorf("cannot apply action in stage %s", e.Stage)
	}
	if seat != e.ActiveSeat {
		return nil, fmt.Errorf("not your turn (active=%d, you=%d)", e.ActiveSeat, seat)
	}
	p := e.playerBySeat(seat)
	if p.State != PlayerActive {
		return nil, fmt.Errorf("player %d state %s cannot act", seat, p.State)
	}

	var actualAmount int

	switch action.Type {
	case ActionFold:
		p.State = PlayerFolded
		p.HasActed = true

	case ActionCheck:
		if p.BetThisRound != e.CurrentBet {
			return nil, fmt.Errorf("cannot check, must call %d", e.CurrentBet-p.BetThisRound)
		}
		p.HasActed = true

	case ActionCall:
		diff := e.CurrentBet - p.BetThisRound
		if diff == 0 {
			p.HasActed = true
			break
		}
		actualAmount = diff
		if p.Chips <= diff {
			actualAmount = p.Chips
			p.State = PlayerAllIn
		}
		e.movePlayerChips(p, actualAmount)
		p.HasActed = true

	case ActionRaise:
		target := action.Amount
		if target <= e.CurrentBet {
			return nil, fmt.Errorf("raise target %d must exceed current bet %d", target, e.CurrentBet)
		}
		putIn := target - p.BetThisRound
		if putIn > p.Chips {
			return nil, fmt.Errorf("not enough chips: need %d, have %d", putIn, p.Chips)
		}
		raiseSize := target - e.CurrentBet
		fullRaise := raiseSize >= e.LastRaiseSize
		// Short raise allowed only if it's an all-in (player has no chips left)
		if !fullRaise && putIn != p.Chips {
			return nil, fmt.Errorf("min raise size is %d, got %d", e.LastRaiseSize, raiseSize)
		}
		actualAmount = putIn
		e.movePlayerChips(p, putIn)
		if p.Chips == 0 {
			p.State = PlayerAllIn
		}
		e.CurrentBet = target
		if fullRaise {
			e.LastRaiseSize = raiseSize
			e.reopenAction(seat)
		}
		p.HasActed = true

	case ActionAllIn:
		if p.Chips == 0 {
			return nil, errors.New("no chips to all-in")
		}
		actualAmount = p.Chips
		target := p.BetThisRound + p.Chips
		e.movePlayerChips(p, p.Chips)
		p.State = PlayerAllIn
		if target > e.CurrentBet {
			raiseSize := target - e.CurrentBet
			e.CurrentBet = target
			if raiseSize >= e.LastRaiseSize {
				e.LastRaiseSize = raiseSize
				e.reopenAction(seat)
			}
		}
		p.HasActed = true

	default:
		return nil, fmt.Errorf("unknown action type %v", action.Type)
	}

	events := []Event{{Type: EventAction, Data: map[string]any{
		"seat": seat, "type": action.Type.String(), "amount": actualAmount,
	}}}

	// Hand-end shortcut: only one player remains in the hand
	if e.countInHand() == 1 {
		e.ActiveSeat = -1
		return events, nil
	}

	if e.bettingRoundClosed() {
		e.ActiveSeat = -1
	} else {
		e.ActiveSeat = e.nextActiveSeat(seat)
	}

	return events, nil
}

// ============================================================
// AdvanceStage: deal next street, or run showdown
// ============================================================

func (e *Engine) AdvanceStage() ([]Event, error) {
	if e.ActiveSeat != -1 {
		return nil, errors.New("betting round still open")
	}

	// If only one player remains, award uncontested
	if e.countInHand() == 1 {
		return e.endHandUncontested()
	}

	// River → Showdown
	if e.Stage == StageRiver {
		return e.runShowdown()
	}

	var nextStage Stage
	var dealCount int
	switch e.Stage {
	case StagePreflop:
		nextStage, dealCount = StageFlop, 3
	case StageFlop:
		nextStage, dealCount = StageTurn, 1
	case StageTurn:
		nextStage, dealCount = StageRiver, 1
	default:
		return nil, fmt.Errorf("cannot advance from stage %s", e.Stage)
	}

	// Burn one card before dealing community
	if _, err := e.deck.Deal(1); err != nil {
		return nil, err
	}
	cards, err := e.deck.Deal(dealCount)
	if err != nil {
		return nil, err
	}
	e.Community = append(e.Community, cards...)
	e.Stage = nextStage

	events := []Event{{Type: EventCommunityDealt, Data: map[string]any{
		"cards": cards, "stage": nextStage.String(),
	}}}

	// Reset round state
	for _, p := range e.Players {
		p.BetThisRound = 0
		p.HasActed = false
	}
	e.CurrentBet = 0
	e.LastRaiseSize = e.BigBlind

	// First to act post-flop: first Active clockwise from dealer
	e.ActiveSeat = e.nextActiveSeat(e.DealerSeat)

	// If 0 or 1 players can act (everyone else all-in), skip betting and recurse
	if e.countActive() < 2 {
		e.ActiveSeat = -1
		more, err := e.AdvanceStage()
		events = append(events, more...)
		return events, err
	}

	events = append(events, Event{Type: EventStageStart, Data: map[string]any{
		"stage": nextStage.String(), "first": e.ActiveSeat,
	}})
	return events, nil
}

// ============================================================
// Hand termination
// ============================================================

func (e *Engine) endHandUncontested() ([]Event, error) {
	var winner *EnginePlayer
	for _, p := range e.Players {
		if p.State == PlayerActive || p.State == PlayerAllIn {
			winner = p
			break
		}
	}
	if winner == nil {
		return nil, errors.New("no players in hand")
	}
	awarded := e.Pot
	winner.Chips += awarded
	e.Pot = 0
	e.Stage = StageHandComplete
	return []Event{{Type: EventHandComplete, Data: map[string]any{
		"uncontested": true, "winner": winner.Seat, "amount": awarded,
	}}}, nil
}

// PlayerHandSummary is included in the showdown event so clients can render
// each contestant's evaluated hand without re-running evaluation locally.
type PlayerHandSummary struct {
	PlayerID  string `json:"playerId"`
	Rank      string `json:"rank"`      // e.g. "full-house"
	HoleCards []Card `json:"holeCards"` // revealed
}

func (e *Engine) runShowdown() ([]Event, error) {
	contribs := make([]PotContribution, 0, len(e.Players))
	hands := make([]PlayerHandSummary, 0, len(e.Players))
	for _, p := range e.Players {
		if p.Committed == 0 {
			continue
		}
		var hand HandValue
		folded := p.State == PlayerFolded
		if !folded {
			cards := append([]Card{}, p.HoleCards...)
			cards = append(cards, e.Community...)
			hand = EvaluateBest5(cards)
			hands = append(hands, PlayerHandSummary{
				PlayerID:  p.ID,
				Rank:      hand.Rank.Slug(),
				HoleCards: append([]Card{}, p.HoleCards...),
			})
		}
		contribs = append(contribs, PotContribution{
			PlayerID: p.ID, Committed: p.Committed, Folded: folded, Hand: hand,
		})
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		return nil, err
	}
	for _, share := range shares {
		for _, p := range e.Players {
			if p.ID == share.PlayerID {
				p.Chips += share.Amount
				break
			}
		}
	}
	e.Pot = 0
	e.Stage = StageHandComplete
	return []Event{
		{Type: EventShowdown, Data: map[string]any{
			"community": e.Community,
			"shares":    shares,
			"hands":     hands,
		}},
		{Type: EventHandComplete, Data: map[string]any{"uncontested": false}},
	}, nil
}

// ============================================================
// Helpers
// ============================================================

func (e *Engine) playerBySeat(seat int) *EnginePlayer {
	for _, p := range e.Players {
		if p.Seat == seat {
			return p
		}
	}
	panic(fmt.Sprintf("no player at seat %d", seat))
}

func (e *Engine) countActive() int {
	n := 0
	for _, p := range e.Players {
		if p.State == PlayerActive {
			n++
		}
	}
	return n
}

func (e *Engine) countInHand() int {
	n := 0
	for _, p := range e.Players {
		if p.State == PlayerActive || p.State == PlayerAllIn {
			n++
		}
	}
	return n
}

// nextActiveSeat returns the next seat clockwise (by ascending Seat number with wraparound)
// whose State is PlayerActive. Returns -1 if none.
func (e *Engine) nextActiveSeat(fromSeat int) int {
	n := len(e.Players)
	idx := -1
	for i, p := range e.Players {
		if p.Seat == fromSeat {
			idx = i
			break
		}
	}
	for i := 1; i <= n; i++ {
		j := (idx + i + n) % n
		if e.Players[j].State == PlayerActive {
			return e.Players[j].Seat
		}
	}
	return -1
}

// nextSeatInHand is like nextActiveSeat but includes AllIn players (used for dealing).
func (e *Engine) nextSeatInHand(fromSeat int) int {
	n := len(e.Players)
	idx := -1
	for i, p := range e.Players {
		if p.Seat == fromSeat {
			idx = i
			break
		}
	}
	for i := 1; i <= n; i++ {
		j := (idx + i + n) % n
		if e.Players[j].State == PlayerActive || e.Players[j].State == PlayerAllIn {
			return e.Players[j].Seat
		}
	}
	return -1
}

func (e *Engine) postForced(seat, amount int) int {
	p := e.playerBySeat(seat)
	actual := amount
	if p.Chips < amount {
		actual = p.Chips
	}
	e.movePlayerChips(p, actual)
	if p.Chips == 0 && actual > 0 {
		p.State = PlayerAllIn
	}
	return actual
}

// movePlayerChips moves `amount` from the player's stack into the pot.
func (e *Engine) movePlayerChips(p *EnginePlayer, amount int) {
	p.Chips -= amount
	p.BetThisRound += amount
	p.Committed += amount
	e.Pot += amount
}

// reopenAction marks all other Active players as not-yet-acted, so a raise
// gives them another turn. Skips the raiser themself.
func (e *Engine) reopenAction(raiserSeat int) {
	for _, op := range e.Players {
		if op.State == PlayerActive && op.Seat != raiserSeat {
			op.HasActed = false
		}
	}
}

// bettingRoundClosed returns true when no Active player still owes an action
// or a chip difference vs CurrentBet.
func (e *Engine) bettingRoundClosed() bool {
	for _, p := range e.Players {
		if p.State != PlayerActive {
			continue
		}
		if !p.HasActed {
			return false
		}
		if p.BetThisRound != e.CurrentBet {
			return false
		}
	}
	return true
}
