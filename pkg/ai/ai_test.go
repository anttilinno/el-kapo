package ai

import (
	"math/rand"
	"testing"

	"el-kapo/pkg/game"
)

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
