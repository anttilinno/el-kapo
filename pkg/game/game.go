package game

import (
	"fmt"
	"math/rand"
	"sort"
)

// Phase tracks where the current player is within their turn.
type Phase int

const (
	TurnStart Phase = iota // waiting for draw / take-discard / kapo
	Drawn                  // holding a drawn card, must swap or discard it
	Action                 // resolving the special action of a discarded 7-10/J/Q
	Ended                  // round is over
)

// ActionType is the special action unlocked by discarding a drawn 7-10/J/Q.
type ActionType int

const (
	ActionNone ActionType = iota
	ActionPeekOwn
	ActionPeekOther
	ActionSwapOther
)

// Game is a single round of Kapo. Pure engine, no I/O; all randomness comes
// from the injected *rand.Rand so play is deterministic given a seed.
type Game struct {
	players [][]Card
	seen    [][][]bool // seen[viewer][player][slot]: viewer knows that card's identity
	faceUp  [][]bool   // slot is public to everyone; implies seen for all viewers

	deck    []Card
	discard []Card

	current       int
	phase         Phase
	drawnCard     Card
	pendingAction ActionType

	kapoCaller int
	movesLeft  int // turns remaining after kapo before the round ends

	rng *rand.Rand
}

// NewRound deals 4 face-down cards to each of numPlayers players and picks a
// random starting player. Callers must still MarkPeek each player's 2 setup
// peeks.
func NewRound(numPlayers int, rng *rand.Rand) *Game {
	deck := NewDeck(rng)
	players := make([][]Card, numPlayers)
	seen := make([][][]bool, numPlayers)
	faceUp := make([][]bool, numPlayers)
	for p := 0; p < numPlayers; p++ {
		players[p] = append([]Card(nil), deck[:4]...)
		deck = deck[4:]
		faceUp[p] = make([]bool, 4)
		seen[p] = make([][]bool, numPlayers)
		for q := 0; q < numPlayers; q++ {
			seen[p][q] = make([]bool, 4)
		}
	}
	return &Game{
		players:    players,
		seen:       seen,
		faceUp:     faceUp,
		deck:       deck,
		current:    rng.Intn(numPlayers),
		phase:      TurnStart,
		kapoCaller: -1,
		rng:        rng,
	}
}

// NewRoundWithStarter is like NewRound but forces the first player instead of
// choosing at random (used to let the previous round's winner lead).
func NewRoundWithStarter(numPlayers, starter int, rng *rand.Rand) *Game {
	g := NewRound(numPlayers, rng)
	g.current = starter
	return g
}

// MarkPeek records that player has seen their own slot and returns the card.
func (g *Game) MarkPeek(player, slot int) Card {
	g.seen[player][player][slot] = true
	return g.players[player][slot]
}

// SeedForTest replaces the current player's hand (all slots marked seen by the
// owner) and the discard pile, for deterministic AI/game tests.
func (g *Game) SeedForTest(hand, discard []Card) {
	cur := g.current
	g.players[cur] = append([]Card(nil), hand...)
	g.faceUp[cur] = make([]bool, len(hand))
	for v := range g.seen {
		g.seen[v][cur] = make([]bool, len(hand))
	}
	for s := range hand {
		g.seen[cur][cur][s] = true
	}
	g.discard = append([]Card(nil), discard...)
}

func (g *Game) validSlot(player, slot int) bool {
	return slot >= 0 && slot < len(g.players[player])
}

// endTurn advances to the next player, or ends the round if the deck just
// ran out or the post-kapo move budget is exhausted.
func (g *Game) endTurn() {
	if len(g.deck) == 0 {
		g.phase = Ended
		return
	}
	if g.kapoCaller != -1 {
		g.movesLeft--
		if g.movesLeft <= 0 {
			g.phase = Ended
			return
		}
	}
	g.current = (g.current + 1) % len(g.players)
	g.phase = TurnStart
}

// Draw takes the top deck card into the current player's hand (undecided
// slot yet); it must be followed by SwapDrawn, MultiSwapDrawn or DiscardDrawn.
func (g *Game) Draw() (Card, error) {
	if g.phase != TurnStart {
		return Card{}, fmt.Errorf("cannot draw now")
	}
	if len(g.deck) == 0 {
		return Card{}, fmt.Errorf("deck is empty")
	}
	g.drawnCard = g.deck[len(g.deck)-1]
	g.deck = g.deck[:len(g.deck)-1]
	g.phase = Drawn
	return g.drawnCard, nil
}

// SwapDrawn places the drawn card into slot, discarding the card it replaces.
func (g *Game) SwapDrawn(slot int) error {
	if g.phase != Drawn {
		return fmt.Errorf("cannot swap now")
	}
	if !g.validSlot(g.current, slot) {
		return fmt.Errorf("invalid slot")
	}
	old := g.players[g.current][slot]
	g.players[g.current][slot] = g.drawnCard
	// only the owner saw the drawn card before it went face-down
	for v := range g.seen {
		g.seen[v][g.current][slot] = v == g.current
	}
	g.faceUp[g.current][slot] = false
	g.discard = append(g.discard, old)
	g.endTurn()
	return nil
}

// DiscardDrawn puts the drawn card face-up on the discard pile and reports
// which special action (if any) the player may now take.
func (g *Game) DiscardDrawn() (ActionType, error) {
	if g.phase != Drawn {
		return ActionNone, fmt.Errorf("cannot discard now")
	}
	g.discard = append(g.discard, g.drawnCard)
	at := actionFor(g.drawnCard.Rank)
	if at == ActionNone {
		g.endTurn()
		return ActionNone, nil
	}
	g.pendingAction = at
	g.phase = Action
	return at, nil
}

func actionFor(r Rank) ActionType {
	switch r {
	case 7, 8:
		return ActionPeekOwn
	case 9, 10:
		return ActionPeekOther
	case Jack, Queen:
		return ActionSwapOther
	default:
		return ActionNone
	}
}

// MultiSwapResult reports the outcome of a multi-swap attempt.
type MultiSwapResult struct {
	Success  bool
	Cards    []Card // discarded cards on success, revealed (now face-up) cards on mismatch
	HandSize int    // resulting hand size of the acting player
}

// checkMultiSlots validates 2+ distinct in-range slots for the current player.
func (g *Game) checkMultiSlots(slots []int) error {
	if len(slots) < 2 {
		return fmt.Errorf("multi-swap needs at least 2 slots")
	}
	seen := make(map[int]bool, len(slots))
	for _, s := range slots {
		if !g.validSlot(g.current, s) {
			return fmt.Errorf("invalid slot")
		}
		if seen[s] {
			return fmt.Errorf("duplicate slot")
		}
		seen[s] = true
	}
	return nil
}

// addCard appends a card face-down to the player's hand. If public, every
// viewer watched the card go in (e.g. it came off the discard pile);
// otherwise only the owner knows it.
func (g *Game) addCard(player int, c Card, public bool) {
	g.players[player] = append(g.players[player], c)
	g.faceUp[player] = append(g.faceUp[player], false)
	for v := range g.seen {
		g.seen[v][player] = append(g.seen[v][player], public || v == player)
	}
}

// resolveMultiSwap applies the multi-swap rule with pulled as the incoming
// card. Success (all selected same rank): selected cards are discarded and
// the hand shrinks. Mismatch: selected cards stay but flip permanently face
// up, and the pulled card still joins the hand, growing it by one.
func (g *Game) resolveMultiSwap(slots []int, pulled Card, pulledPublic bool) MultiSwapResult {
	cur := g.current
	cards := make([]Card, len(slots))
	same := true
	for i, s := range slots {
		cards[i] = g.players[cur][s]
		if cards[i].Rank != cards[0].Rank {
			same = false
		}
	}
	if same {
		// remove selected slots highest-first so earlier indices stay valid
		desc := append([]int(nil), slots...)
		sort.Sort(sort.Reverse(sort.IntSlice(desc)))
		for _, s := range desc {
			g.players[cur] = append(g.players[cur][:s], g.players[cur][s+1:]...)
			g.faceUp[cur] = append(g.faceUp[cur][:s], g.faceUp[cur][s+1:]...)
			for v := range g.seen {
				g.seen[v][cur] = append(g.seen[v][cur][:s], g.seen[v][cur][s+1:]...)
			}
		}
		g.discard = append(g.discard, cards...)
	} else {
		// flipped cards are public: everyone sees them from now on
		for _, s := range slots {
			g.faceUp[cur][s] = true
			for v := range g.seen {
				g.seen[v][cur][s] = true
			}
		}
	}
	g.addCard(cur, pulled, pulledPublic)
	g.endTurn()
	return MultiSwapResult{Success: same, Cards: cards, HandSize: len(g.players[cur])}
}

// MultiSwapDrawn swaps all selected slots out for the drawn card.
func (g *Game) MultiSwapDrawn(slots []int) (MultiSwapResult, error) {
	if g.phase != Drawn {
		return MultiSwapResult{}, fmt.Errorf("cannot multi-swap now")
	}
	if err := g.checkMultiSlots(slots); err != nil {
		return MultiSwapResult{}, err
	}
	return g.resolveMultiSwap(slots, g.drawnCard, false), nil
}

// MultiSwapDiscard swaps all selected slots out for the top face-up discard.
func (g *Game) MultiSwapDiscard(slots []int) (MultiSwapResult, error) {
	if g.phase != TurnStart {
		return MultiSwapResult{}, fmt.Errorf("cannot multi-swap now")
	}
	if len(g.discard) == 0 {
		return MultiSwapResult{}, fmt.Errorf("discard pile is empty")
	}
	if err := g.checkMultiSlots(slots); err != nil {
		return MultiSwapResult{}, err
	}
	pulled := g.discard[len(g.discard)-1]
	g.discard = g.discard[:len(g.discard)-1]
	// pulled off the face-up discard: everyone watched where it went
	return g.resolveMultiSwap(slots, pulled, true), nil
}

// PeekOwn resolves a pending 7/8 action: look at one of your own slots.
// ponytail: rules say "unknown" card, but we don't enforce that here - an
// already-known slot is just a no-op reveal; simplest to let the caller decide.
func (g *Game) PeekOwn(slot int) (Card, error) {
	if g.phase != Action || g.pendingAction != ActionPeekOwn {
		return Card{}, fmt.Errorf("no peek-own action pending")
	}
	if !g.validSlot(g.current, slot) {
		return Card{}, fmt.Errorf("invalid slot")
	}
	c := g.players[g.current][slot]
	g.seen[g.current][g.current][slot] = true
	g.endTurn()
	return c, nil
}

// PeekOther resolves a pending 9/10 action: look at one opponent's slot.
// The peeker remembers it; the opponent's own knowledge is unchanged.
func (g *Game) PeekOther(player, slot int) (Card, error) {
	if g.phase != Action || g.pendingAction != ActionPeekOther {
		return Card{}, fmt.Errorf("no peek-other action pending")
	}
	if player == g.current || player < 0 || player >= len(g.players) {
		return Card{}, fmt.Errorf("invalid player")
	}
	if !g.validSlot(player, slot) {
		return Card{}, fmt.Errorf("invalid slot")
	}
	c := g.players[player][slot]
	g.seen[g.current][player][slot] = true
	g.endTurn()
	return c, nil
}

// SwapOther resolves a pending J/Q action: blind-swap own slot with an
// opponent's slot. Everyone watches which physical card moves where, so each
// viewer's knowledge follows the cards: whoever knew a card before the swap
// still knows it at its new location. Face-up status moves with the card too.
func (g *Game) SwapOther(ownSlot, player, theirSlot int) error {
	if g.phase != Action || g.pendingAction != ActionSwapOther {
		return fmt.Errorf("no swap-other action pending")
	}
	if player == g.current || player < 0 || player >= len(g.players) {
		return fmt.Errorf("invalid player")
	}
	if !g.validSlot(g.current, ownSlot) || !g.validSlot(player, theirSlot) {
		return fmt.Errorf("invalid slot")
	}
	cur := g.current
	g.players[cur][ownSlot], g.players[player][theirSlot] =
		g.players[player][theirSlot], g.players[cur][ownSlot]
	g.faceUp[cur][ownSlot], g.faceUp[player][theirSlot] =
		g.faceUp[player][theirSlot], g.faceUp[cur][ownSlot]
	for v := range g.seen {
		g.seen[v][cur][ownSlot], g.seen[v][player][theirSlot] =
			g.seen[v][player][theirSlot], g.seen[v][cur][ownSlot]
	}
	g.endTurn()
	return nil
}

// SkipAction declines a pending special action.
func (g *Game) SkipAction() error {
	if g.phase != Action {
		return fmt.Errorf("no action pending")
	}
	g.endTurn()
	return nil
}

// TakeDiscard swaps the top discard into slot instead of drawing. The card
// it replaces becomes the new (face-up) top of the discard pile.
func (g *Game) TakeDiscard(slot int) error {
	if g.phase != TurnStart {
		return fmt.Errorf("cannot take discard now")
	}
	if len(g.discard) == 0 {
		return fmt.Errorf("discard pile is empty")
	}
	if !g.validSlot(g.current, slot) {
		return fmt.Errorf("invalid slot")
	}
	top := g.discard[len(g.discard)-1]
	old := g.players[g.current][slot]
	g.players[g.current][slot] = top
	g.discard[len(g.discard)-1] = old
	// taken off the face-up discard: everyone watched which card went where
	for v := range g.seen {
		g.seen[v][g.current][slot] = true
	}
	g.faceUp[g.current][slot] = false
	g.endTurn()
	return nil
}

// CallKapo ends the caller's own turn immediately and gives every other
// player exactly one more turn before the round ends.
func (g *Game) CallKapo() error {
	if g.phase != TurnStart {
		return fmt.Errorf("cannot call kapo now")
	}
	if g.kapoCaller != -1 {
		return fmt.Errorf("kapo already called")
	}
	g.kapoCaller = g.current
	g.movesLeft = len(g.players) - 1
	if g.movesLeft <= 0 {
		g.phase = Ended
		return nil
	}
	g.current = (g.current + 1) % len(g.players)
	g.phase = TurnStart
	return nil
}

// Accessors.

func (g *Game) Current() int    { return g.current }
func (g *Game) Phase() Phase    { return g.phase }
func (g *Game) DeckLen() int    { return len(g.deck) }
func (g *Game) NumPlayers() int { return len(g.players) }
func (g *Game) KapoCaller() int { return g.kapoCaller }
func (g *Game) Ended() bool     { return g.phase == Ended }

// Hand returns the player's current hand. ponytail: shared slice, callers
// must not mutate it.
func (g *Game) Hand(player int) []Card { return g.players[player] }

func (g *Game) TopDiscard() (Card, bool) {
	if len(g.discard) == 0 {
		return Card{}, false
	}
	return g.discard[len(g.discard)-1], true
}

// KnownCard is the owner's private knowledge of their own slot.
func (g *Game) KnownCard(player, slot int) (Card, bool) {
	return g.KnownTo(player, player, slot)
}

// KnownTo reports whether viewer knows the card at player's slot (via own
// peeks, opponent peeks, or public moves) and returns it if so.
func (g *Game) KnownTo(viewer, player, slot int) (Card, bool) {
	if !g.seen[viewer][player][slot] {
		return Card{}, false
	}
	return g.players[player][slot], true
}

// FaceUpCard is public information: any viewer may see a face-up slot.
func (g *Game) FaceUpCard(player, slot int) (Card, bool) {
	if !g.faceUp[player][slot] {
		return Card{}, false
	}
	return g.players[player][slot], true
}

// RoundResult is the outcome of a finished round.
type RoundResult struct {
	Totals  []int // hand point total per player
	Winners []int // player indices with the lowest total (or the auto-winner)
	AutoWin int   // player index with 2 queens + 2 kings, or -1
	Points  []int // match points earned this round per player (0 for winners)
}

// Results scores the finished round: hasAutoWinHand overrides the normal
// lowest-total win condition.
func (g *Game) Results() RoundResult {
	n := len(g.players)
	totals := make([]int, n)
	for p := 0; p < n; p++ {
		for _, c := range g.players[p] {
			totals[p] += c.Points()
		}
	}

	autoWin := -1
	for p := 0; p < n; p++ {
		if hasAutoWinHand(g.players[p]) {
			autoWin = p
			break
		}
	}

	var winners []int
	if autoWin >= 0 {
		winners = []int{autoWin}
	} else {
		min := totals[0]
		for _, t := range totals[1:] {
			if t < min {
				min = t
			}
		}
		for p, t := range totals {
			if t == min {
				winners = append(winners, p)
			}
		}
	}

	winSet := make(map[int]bool, len(winners))
	for _, w := range winners {
		winSet[w] = true
	}
	points := make([]int, n)
	for p := 0; p < n; p++ {
		if !winSet[p] {
			points[p] = totals[p]
		}
	}

	return RoundResult{Totals: totals, Winners: winners, AutoWin: autoWin, Points: points}
}

// hasAutoWinHand: a hand of exactly 2 queens + 2 kings auto-wins.
func hasAutoWinHand(hand []Card) bool {
	if len(hand) != 4 {
		return false
	}
	q, k := 0, 0
	for _, c := range hand {
		if c.Rank == Queen {
			q++
		}
		if c.Rank == King {
			k++
		}
	}
	return q == 2 && k == 2
}

// Match accumulates round scores across a full game.
type Match struct {
	Scores []int
}

func NewMatch(n int) *Match {
	return &Match{Scores: make([]int, n)}
}

// Apply adds this round's points to each player's score; a score landing on
// exactly 100 drops back to 50.
func (m *Match) Apply(r RoundResult) {
	for p, pts := range r.Points {
		m.Scores[p] += pts
		if m.Scores[p] == 100 {
			m.Scores[p] = 50
		}
	}
}

// Over reports whether any player's score has exceeded 100.
func (m *Match) Over() bool {
	for _, s := range m.Scores {
		if s > 100 {
			return true
		}
	}
	return false
}

// Winner returns the player index with the least total points.
func (m *Match) Winner() int {
	w := 0
	for p, s := range m.Scores {
		if s < m.Scores[w] {
			w = p
		}
	}
	return w
}
