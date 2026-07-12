package game

import (
	"math/rand"
	"testing"
)

func TestNewDeckComposition(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	deck := NewDeck(rng)
	if len(deck) != 52 {
		t.Fatalf("expected 52 cards, got %d", len(deck))
	}
	counts := map[Rank]int{}
	for _, c := range deck {
		counts[c.Rank]++
	}
	if counts[King] != 2 {
		t.Errorf("expected 2 kings, got %d", counts[King])
	}
	if counts[Joker] != 2 {
		t.Errorf("expected 2 jokers, got %d", counts[Joker])
	}
	for r := Ace; r <= Queen; r++ {
		if counts[r] != 4 {
			t.Errorf("rank %d: expected 4, got %d", r, counts[r])
		}
	}
	if (Card{King, Spades}).Points() != 13 {
		t.Errorf("king should be worth 13")
	}
	if (Card{Joker, NoSuit}).Points() != 0 {
		t.Errorf("joker should be worth 0")
	}
}

func TestAutoWinDetection(t *testing.T) {
	autoWin := []Card{{Queen, Clubs}, {Queen, Diamonds}, {King, Spades}, {King, Hearts}}
	if !hasAutoWinHand(autoWin) {
		t.Errorf("2 queens + 2 kings should auto-win")
	}
	normal := []Card{{Queen, Clubs}, {Queen, Diamonds}, {King, Spades}, {Ace, Hearts}}
	if hasAutoWinHand(normal) {
		t.Errorf("should not auto-win without exactly 2 kings")
	}
	short := []Card{{Queen, Clubs}, {Queen, Diamonds}, {King, Spades}}
	if hasAutoWinHand(short) {
		t.Errorf("hand of wrong size should not auto-win")
	}
}

// TestAutoWinResults checks the full round-scoring path: a 2-queens+2-kings
// hand (50 points, normally the worst possible) beats an opponent with a
// lower total, scores 0 match points, and the loser eats their own total.
func TestAutoWinResults(t *testing.T) {
	g := NewRound(2, rand.New(rand.NewSource(1)))
	g.players[0] = []Card{{Queen, Clubs}, {Queen, Diamonds}, {King, Spades}, {King, Hearts}}
	g.players[1] = []Card{{Ace, Clubs}, {Ace, Diamonds}, {Joker, NoSuit}, {2, Hearts}}

	r := g.Results()
	if r.AutoWin != 0 {
		t.Fatalf("AutoWin = %d, want 0", r.AutoWin)
	}
	if len(r.Winners) != 1 || r.Winners[0] != 0 {
		t.Errorf("Winners = %v, want [0]", r.Winners)
	}
	if r.Totals[0] != 50 || r.Totals[1] != 4 {
		t.Errorf("Totals = %v, want [50 4]", r.Totals)
	}
	if r.Points[0] != 0 {
		t.Errorf("auto-winner should score 0 match points, got %d", r.Points[0])
	}
	if r.Points[1] != 4 {
		t.Errorf("loser should eat their hand total 4, got %d", r.Points[1])
	}
}

// newTestGame builds a 2-player round where the current player holds hand.
func newTestGame(t *testing.T, hand []Card) *Game {
	t.Helper()
	g := NewRound(2, rand.New(rand.NewSource(1)))
	cur := g.Current()
	g.players[cur] = append([]Card(nil), hand...)
	g.faceUp[cur] = make([]bool, len(hand))
	for v := range g.seen {
		g.seen[v][cur] = make([]bool, len(hand))
	}
	return g
}

func TestMultiSwapDrawnSuccess(t *testing.T) {
	g := newTestGame(t, []Card{{9, Clubs}, {9, Diamonds}, {5, Hearts}, {2, Spades}})
	cur := g.Current()
	if _, err := g.Draw(); err != nil {
		t.Fatal(err)
	}
	res, err := g.MultiSwapDrawn([]int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatal("expected success for two 9s")
	}
	if res.HandSize != 3 || len(g.Hand(cur)) != 3 {
		t.Errorf("hand should shrink to 3, got %d", len(g.Hand(cur)))
	}
	// both 9s must be on the discard pile (top two cards)
	if len(g.discard) != 2 || g.discard[0].Rank != 9 || g.discard[1].Rank != 9 {
		t.Errorf("discard should hold the two 9s, got %v", g.discard)
	}
	// pulled card is known to owner, not face up
	last := len(g.Hand(cur)) - 1
	if _, ok := g.KnownCard(cur, last); !ok {
		t.Errorf("pulled card should be known to owner")
	}
	if _, up := g.FaceUpCard(cur, last); up {
		t.Errorf("pulled card should not be face up")
	}
}

func TestMultiSwapDrawnMismatch(t *testing.T) {
	g := newTestGame(t, []Card{{9, Clubs}, {5, Hearts}, {9, Diamonds}, {2, Spades}})
	cur := g.Current()
	if _, err := g.Draw(); err != nil {
		t.Fatal(err)
	}
	res, err := g.MultiSwapDrawn([]int{0, 1}) // 9 and 5: mismatch
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected mismatch for 9 and 5")
	}
	if res.HandSize != 5 || len(g.Hand(cur)) != 5 {
		t.Errorf("hand should grow to 5, got %d", len(g.Hand(cur)))
	}
	for _, s := range []int{0, 1} {
		c, up := g.FaceUpCard(cur, s)
		if !up {
			t.Errorf("slot %d should be face up after mismatch", s)
		}
		if c != g.Hand(cur)[s] {
			t.Errorf("FaceUpCard should return the slot's card")
		}
	}
	if len(g.discard) != 0 {
		t.Errorf("nothing should be discarded on a drawn-card mismatch")
	}
}

func TestMultiSwapDiscard(t *testing.T) {
	// success: top discard removed, selected cards pushed
	g := newTestGame(t, []Card{{9, Clubs}, {9, Diamonds}, {5, Hearts}, {2, Spades}})
	cur := g.Current()
	g.discard = []Card{{3, Clubs}}
	res, err := g.MultiSwapDiscard([]int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.HandSize != 3 {
		t.Fatalf("expected success and hand size 3, got %+v", res)
	}
	if len(g.discard) != 2 { // 3♣ pulled out, two 9s pushed
		t.Errorf("discard should hold 2 cards, got %d", len(g.discard))
	}
	found := false
	for _, c := range g.Hand(cur) {
		if c == (Card{3, Clubs}) {
			found = true
		}
	}
	if !found {
		t.Errorf("pulled 3♣ should be in the hand")
	}

	// mismatch: top discard still removed
	g = newTestGame(t, []Card{{9, Clubs}, {5, Hearts}, {9, Diamonds}, {2, Spades}})
	g.discard = []Card{{3, Clubs}}
	res, err = g.MultiSwapDiscard([]int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || res.HandSize != 5 {
		t.Fatalf("expected mismatch and hand size 5, got %+v", res)
	}
	if len(g.discard) != 0 {
		t.Errorf("pulled card should leave the discard pile on mismatch too")
	}
}

// The reported bug: peek an opponent's card, then a J/Q swap moves that card
// into the peeker's hand - the peeker must still know it.
func TestPeekOtherThenSwapOtherKeepsKnowledge(t *testing.T) {
	g := NewRound(2, rand.New(rand.NewSource(1)))
	peeker := g.Current()
	opp := 1 - peeker

	g.phase = Action
	g.pendingAction = ActionPeekOther
	peeked, err := g.PeekOther(opp, 2)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := g.KnownTo(peeker, opp, 2); !ok || c != peeked {
		t.Fatalf("peeker should know opponent slot 2 after peeking")
	}

	// same player later blind-swaps their slot 0 with that peeked slot
	g.current = peeker
	g.phase = Action
	g.pendingAction = ActionSwapOther
	if err := g.SwapOther(0, opp, 2); err != nil {
		t.Fatal(err)
	}
	c, ok := g.KnownTo(peeker, peeker, 0)
	if !ok {
		t.Fatal("peeked card moved into peeker's hand but shows as unknown (the reported bug)")
	}
	if c != peeked {
		t.Errorf("expected %v in peeker slot 0, got %v", peeked, c)
	}
}

// Knowledge follows a card leaving the owner's hand too.
func TestSwapOtherOwnerStillKnowsMovedCard(t *testing.T) {
	g := NewRound(2, rand.New(rand.NewSource(1)))
	cur := g.Current()
	opp := 1 - cur
	mine := g.MarkPeek(cur, 0)

	g.phase = Action
	g.pendingAction = ActionSwapOther
	if err := g.SwapOther(0, opp, 3); err != nil {
		t.Fatal(err)
	}
	if c, ok := g.KnownTo(cur, opp, 3); !ok || c != mine {
		t.Errorf("original owner should still know their card at opponent slot 3")
	}
	if _, ok := g.KnownTo(cur, cur, 0); ok {
		t.Errorf("incoming blind card should be unknown to its new owner")
	}
}

func TestTakeDiscardKnownToAll(t *testing.T) {
	g := NewRound(3, rand.New(rand.NewSource(1)))
	cur := g.Current()
	g.discard = []Card{{3, Clubs}}
	if err := g.TakeDiscard(1); err != nil {
		t.Fatal(err)
	}
	for v := 0; v < g.NumPlayers(); v++ {
		if c, ok := g.KnownTo(v, cur, 1); !ok || c != (Card{3, Clubs}) {
			t.Errorf("viewer %d should know the publicly taken card", v)
		}
	}
}

func TestSwapDrawnKnownToOwnerOnly(t *testing.T) {
	g := NewRound(2, rand.New(rand.NewSource(1)))
	cur := g.Current()
	opp := 1 - cur
	if _, err := g.Draw(); err != nil {
		t.Fatal(err)
	}
	if err := g.SwapDrawn(2); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.KnownTo(cur, cur, 2); !ok {
		t.Errorf("owner should know the card they drew and placed")
	}
	if _, ok := g.KnownTo(opp, cur, 2); ok {
		t.Errorf("opponent should not know a face-down drawn card")
	}
}

func TestRandomPlayoutTerminates(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		numPlayers := 2 + int(seed%3) // cycles 2,3,4
		g := NewRound(numPlayers, rng)
		for p := 0; p < numPlayers; p++ {
			g.MarkPeek(p, 0)
			g.MarkPeek(p, 1)
		}

		steps := 0
		for !g.Ended() && steps < 500 {
			randomLegalMove(g, rng)
			steps++
			// sweep the whole knowledge matrix to catch seen/hand desyncs
			// after multi-swap shrink/grow (would panic on bad indices)
			for v := 0; v < numPlayers; v++ {
				for p := 0; p < numPlayers; p++ {
					for s := range g.Hand(p) {
						g.KnownTo(v, p, s)
					}
				}
			}
		}
		if !g.Ended() {
			t.Fatalf("seed %d: game did not end within 500 steps", seed)
		}

		result := g.Results()
		winSet := make(map[int]bool, len(result.Winners))
		for _, w := range result.Winners {
			winSet[w] = true
		}
		for p := 0; p < numPlayers; p++ {
			want := 0
			for _, c := range g.Hand(p) {
				want += c.Points()
			}
			if result.Totals[p] != want {
				t.Errorf("seed %d: player %d total mismatch: got %d want %d", seed, p, result.Totals[p], want)
			}
			if winSet[p] && result.Points[p] != 0 {
				t.Errorf("seed %d: winner %d should score 0 points, got %d", seed, p, result.Points[p])
			}
		}
	}
}

// randomLegalMove drives one legal action for the current player, biased
// toward drawing so the deck reliably empties and the round terminates.
func randomLegalMove(g *Game, rng *rand.Rand) {
	me := g.Current()
	handLen := len(g.Hand(me))
	if g.KapoCaller() == -1 && rng.Float64() < 0.03 {
		_ = g.CallKapo()
		return
	}
	if _, ok := g.TopDiscard(); ok && rng.Float64() < 0.1 {
		// random multi-swap gamble from discard
		if rng.Float64() < 0.5 && handLen >= 2 {
			a := rng.Intn(handLen)
			b := (a + 1 + rng.Intn(handLen-1)) % handLen
			_, _ = g.MultiSwapDiscard([]int{a, b})
			return
		}
		_ = g.TakeDiscard(rng.Intn(handLen))
		return
	}
	if _, err := g.Draw(); err != nil {
		return // deck empty; round already ended
	}
	if rng.Float64() < 0.1 && handLen >= 2 {
		// random multi-swap gamble on the drawn card
		a := rng.Intn(handLen)
		b := (a + 1 + rng.Intn(handLen-1)) % handLen
		_, _ = g.MultiSwapDrawn([]int{a, b})
		return
	}
	if rng.Float64() < 0.5 {
		_ = g.SwapDrawn(rng.Intn(handLen))
		return
	}
	at, _ := g.DiscardDrawn()
	me = g.Current() // still mid-turn: phase is Action, current player unchanged
	opp := (me + 1) % g.NumPlayers()
	switch at {
	case ActionPeekOwn:
		_, _ = g.PeekOwn(rng.Intn(len(g.Hand(me))))
	case ActionPeekOther:
		_, _ = g.PeekOther(opp, rng.Intn(len(g.Hand(opp))))
	case ActionSwapOther:
		_ = g.SwapOther(rng.Intn(len(g.Hand(me))), opp, rng.Intn(len(g.Hand(opp))))
	}
	// ActionNone: DiscardDrawn already ended the turn.
}

func TestMatchScoring(t *testing.T) {
	m := NewMatch(2)

	m.Apply(RoundResult{Points: []int{0, 100}})
	if m.Scores[1] != 50 {
		t.Errorf("exactly 100 should drop to 50, got %d", m.Scores[1])
	}
	if m.Over() {
		t.Errorf("match should not be over at 50")
	}

	m.Apply(RoundResult{Points: []int{0, 60}})
	if !m.Over() {
		t.Errorf("match should be over above 100")
	}
	if m.Winner() != 0 {
		t.Errorf("winner should be the player with least points, got %d", m.Winner())
	}
}
