package server

import (
	"math/rand"
	"net/url"
	"strings"
	"testing"
	"time"

	"el-kapo/pkg/game"
)

// TestInfoLeak is the important one: a viewer's rendered board must never
// contain the string of a card they have neither legitimately seen
// (KnownTo) nor that is publicly face-up.
func TestInfoLeak(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	g := game.NewRound(3, rng)
	for p := 0; p < 3; p++ {
		g.MarkPeek(p, 0)
		g.MarkPeek(p, 1)
	}
	// advance a few turns so some knowledge/face-up state exists beyond setup
	for i := 0; i < 3 && !g.Ended(); i++ {
		cur := g.Current()
		if _, err := g.Draw(); err != nil {
			break
		}
		_ = g.SwapDrawn(0)
		_ = cur
	}

	tbl := &Table{
		id:    "test",
		phase: Playing,
		g:     g,
		match: game.NewMatch(3),
		seats: []seat{
			{name: "Alice"},
			{name: "Bob"},
			{name: "AI-1", isBot: true},
		},
		flash:       make([]string, 3),
		rng:         rng,
		nextStarter: -1,
		armedSlot:   -1,
	}

	frag := tbl.RenderFragment(0)

	for p := 1; p < 3; p++ {
		for s := range g.Hand(p) {
			if _, ok := g.KnownTo(0, p, s); ok {
				continue
			}
			if _, ok := g.FaceUpCard(p, s); ok {
				continue
			}
			card := g.Hand(p)[s]
			if card.Rank == game.Joker {
				// both jokers render identically ("Joker") so this isn't a
				// meaningful identity leak even if the string appears
				// elsewhere (e.g. the other joker is visible).
				continue
			}
			if strings.Contains(frag, card.String()) {
				t.Errorf("viewer 0's board leaks unseen card %s (player %d slot %d)", card, p, s)
			}
		}
	}
}

// TestStateTransitions drives a 1-human/1-bot table through
// Peeking -> Playing -> RoundOver -> Peeking (next round) via the public
// ApplyMove entry point, forcing a quick round end via CallKapo so the test
// doesn't depend on how many turns a full round happens to take.
func TestStateTransitions(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	tbl := NewTable(rng)
	tbl.seats = []seat{{name: "Alice"}, {name: "AI-1", isBot: true}}
	tbl.flash = make([]string, 2)
	tbl.match = game.NewMatch(2)
	tbl.startRound(0) // force the human to start, for a deterministic test

	if tbl.Phase() != Peeking {
		t.Fatalf("phase = %v, want Peeking", tbl.Phase())
	}

	tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"0"}})
	tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"1"}})

	if tbl.Phase() != Playing {
		t.Fatalf("phase = %v, want Playing after 2 setup peeks", tbl.Phase())
	}
	if tbl.g.Current() != 0 {
		t.Fatalf("current player = %d, want 0 (human)", tbl.g.Current())
	}

	tbl.ApplyMove(0, "kapo", nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p := tbl.Phase(); p == RoundOver || p == MatchOver {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	phase := tbl.Phase()
	if phase != RoundOver && phase != MatchOver {
		t.Fatalf("phase = %v, want RoundOver or MatchOver after Kapo plays out", phase)
	}
	if phase != RoundOver {
		return // match already over (score edge case) - nothing more to assert
	}

	tbl.ApplyMove(0, "next-round", nil)
	if tbl.Phase() != Peeking {
		t.Fatalf("phase = %v, want Peeking after next-round", tbl.Phase())
	}
}
