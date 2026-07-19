package ai

import (
	"math/rand"
	"testing"

	"el-kapo/pkg/game"
)

// Repro: the AI called Kapo holding A, Q, Joker (13 points) because the Queen
// slot was unknown and the optimistic planning estimate valued it at 7. The
// kapo decision must be pessimistic about unseen slots, and never wrong about
// a hand it fully knows.
func TestKapoNotCalledOnUnknownHighSlot(t *testing.T) {
	// fully-known 13-point hand: must not call kapo.
	g := game.NewRound(2, rand.New(rand.NewSource(1)))
	me := g.Current()
	g.SeedForTest(
		[]game.Card{{Rank: game.Ace, Suit: game.Clubs}, {Rank: game.Queen, Suit: game.Hearts}, {Rank: game.Joker, Suit: game.NoSuit}},
		[]game.Card{{Rank: game.King, Suit: game.Spades}},
	)
	Turn(g, me, rand.New(rand.NewSource(1)), []string{"you", "AI"})
	if g.KapoCaller() == me {
		t.Fatal("AI called kapo on a known 13-point hand (A, Q, Joker)")
	}

	// an unknown slot must be valued more pessimistically for the kapo decision
	// than for ordinary planning, so a mostly-unseen hand isn't gambled on.
	g2 := game.NewRound(2, rand.New(rand.NewSource(2)))
	me2 := g2.Current()
	g2.MarkPeek(me2, 0) // one slot known, the rest unknown
	if kapoEstimate(g2, me2) <= estimateHand(g2, me2) {
		t.Fatalf("kapo estimate (%d) must exceed planning estimate (%d) with unknown slots",
			kapoEstimate(g2, me2), estimateHand(g2, me2))
	}
}

// Guard the other side: a fully-known genuinely low hand still calls Kapo.
func TestKapoCalledWhenKnownLow(t *testing.T) {
	g := game.NewRound(2, rand.New(rand.NewSource(1)))
	me := g.Current()
	g.SeedForTest(
		[]game.Card{{Rank: game.Ace, Suit: game.Clubs}, {Rank: 2, Suit: game.Hearts}, {Rank: game.Joker, Suit: game.NoSuit}},
		[]game.Card{{Rank: game.King, Suit: game.Spades}},
	)
	Turn(g, me, rand.New(rand.NewSource(1)), []string{"you", "AI"})
	if g.KapoCaller() != me {
		t.Fatal("AI failed to call kapo on a known 3-point hand (A, 2, Joker)")
	}
}

func otherSuit(s game.Suit) game.Suit {
	if s == game.Spades {
		return game.Clubs
	}
	return game.Spades
}

// A high card matching one the AI already knows should be built into a set by
// sacrificing an unknown slot - a speculative move that sets up dumping the
// whole set later for one low card. Low sets are not worth the gamble.
func TestBuildsHighSetOntoUnknownSlot(t *testing.T) {
	built := false
	for seed := int64(1); seed < 300 && !built; seed++ {
		g := game.NewRound(2, rand.New(rand.NewSource(seed)))
		me := g.Current()
		c := g.MarkPeek(me, 0) // slot 0 known; the other slots stay unknown
		inc := game.Card{Rank: c.Rank, Suit: otherSuit(c.Suit)}

		slot, ok := buildTargetSlot(g, me, inc)
		if c.Points() >= buildSetMinPoints && c.Rank != game.Jack && c.Rank != game.Queen {
			if !ok {
				t.Fatalf("seed %d: should build a %v-set, but declined", seed, c.Rank)
			}
			if _, known := g.KnownCard(me, slot); known {
				t.Fatalf("seed %d: build must sacrifice an unknown slot, got known slot %d", seed, slot)
			}
			built = true
		} else if ok {
			t.Fatalf("seed %d: must not speculatively build a %v-set", seed, c.Rank)
		}
	}
	if !built {
		t.Skip("no high card dealt to slot 0 in seed range")
	}
}

// The reported bug: holding 4,4,5 with a 4 on the discard, the AI multi-swapped
// the pair of 4s for the single discard 4 (shedding one card) instead of taking
// the 4 onto the 5 to make 4,4,4 (setting up a triple multi-swap that sheds two).
func TestGrowsGroupInsteadOfConsumingIt(t *testing.T) {
	g := game.NewRound(2, rand.New(rand.NewSource(1)))
	me := g.Current()
	g.SeedForTest(
		[]game.Card{{Rank: 4, Suit: game.Clubs}, {Rank: 4, Suit: game.Diamonds}, {Rank: 5, Suit: game.Hearts}},
		[]game.Card{{Rank: 4, Suit: game.Spades}},
	)

	Turn(g, me, rand.New(rand.NewSource(1)), []string{"you", "AI"})

	fours := 0
	for _, c := range g.Hand(me) {
		if c.Rank == 4 {
			fours++
		}
	}
	if fours != 3 || len(g.Hand(me)) != 3 {
		t.Fatalf("expected hand of three 4s, got %v", g.Hand(me))
	}
	if top, _ := g.TopDiscard(); top.Rank != 5 {
		t.Fatalf("expected the 5 discarded, top is %v", top)
	}
}
