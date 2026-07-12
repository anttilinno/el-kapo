package ai

import (
	"math/rand"
	"testing"

	"kapo/pkg/game"
)

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
