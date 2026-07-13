package server

import (
	"math/rand"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"el-kapo/pkg/game"
)

// TestMain runs bot turns near-instantly so timing tests don't pay the
// human-facing 1400ms pace.
func TestMain(m *testing.M) {
	botTick = time.Millisecond
	os.Exit(m.Run())
}

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

// hardTablePlaying returns a hard-mode 1-human/1-bot table advanced past the
// two setup peeks into Playing, with the human (seat 0) to move.
func hardTablePlaying(t *testing.T, seed int64) *Table {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	tbl := NewTable(rng)
	tbl.hard = true
	tbl.seats = []seat{{name: "Alice"}, {name: "AI-1", isBot: true}}
	tbl.flash = make([]string, 2)
	tbl.match = game.NewMatch(2)
	tbl.startRound(0)
	tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"0"}})
	tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"1"}})
	if tbl.Phase() != Playing || tbl.g.Current() != 0 {
		t.Fatalf("setup failed: phase=%v current=%d", tbl.Phase(), tbl.g.Current())
	}
	return tbl
}

// TestHardModeSwapNoReveal: in hard mode, swapping the drawn card into a slot
// makes it known to the player but must NOT reveal it - the board renders it
// face-down and the player has to remember it.
func TestHardModeSwapNoReveal(t *testing.T) {
	tbl := hardTablePlaying(t, 7)
	// swap into slot 2 (not one of the peeked setup slots 0/1) to isolate.
	tbl.ApplyMove(0, "draw", nil)
	tbl.ApplyMove(0, "swap", url.Values{"slot": {"2"}})

	if _, known := tbl.g.KnownTo(0, 0, 2); !known {
		t.Fatal("precondition: slot 2 should be known to its owner after a swap")
	}
	cv := tbl.cardView(0, 0, 2)
	if cv.Show {
		t.Error("hard mode: swapped-in card is shown; it must stay face-down")
	}
}

// TestHardModeSetupPeekReveals: the intentional starting-pair peek DOES grant
// a (timed) reveal, so the peeked slot is shown right after peeking.
func TestHardModeSetupPeekReveals(t *testing.T) {
	tbl := hardTablePlaying(t, 7)
	cv := tbl.cardView(0, 0, 0) // slot 0 was peeked during setup
	if !cv.Show {
		t.Error("hard mode: setup-peeked card should be revealed")
	}
}

// TestEasyModeSwapReveals guards the boundary: the no-reveal rule is
// hard-mode-only; in easy mode a swapped-in card stays visible.
func TestEasyModeSwapReveals(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	tbl := NewTable(rng)
	tbl.seats = []seat{{name: "Alice"}, {name: "AI-1", isBot: true}}
	tbl.flash = make([]string, 2)
	tbl.match = game.NewMatch(2)
	tbl.startRound(0)
	tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"0"}})
	tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"1"}})
	tbl.ApplyMove(0, "draw", nil)
	tbl.ApplyMove(0, "swap", url.Values{"slot": {"2"}})

	cv := tbl.cardView(0, 0, 2)
	if !cv.Show {
		t.Error("easy mode: swapped-in card should stay visible")
	}
}

// TestHardModeFaceUpPublic: cards flipped face-up by a failed multi-swap are a
// physical public penalty - shown to everyone in hard mode with no reveal
// window, since they are not a memory reveal.
func TestHardModeFaceUpPublic(t *testing.T) {
	tbl := hardTablePlaying(t, 7)
	// find two of the human's slots with different ranks so a multi-swap
	// mismatches and flips them face-up.
	hand := tbl.g.Hand(0)
	a, b := -1, -1
	for i := range hand {
		for j := i + 1; j < len(hand); j++ {
			if hand[i].Rank != hand[j].Rank {
				a, b = i, j
				break
			}
		}
		if a >= 0 {
			break
		}
	}
	if a < 0 {
		t.Skip("all four cards share a rank; cannot force a mismatch this seed")
	}
	tbl.ApplyMove(0, "draw", nil)
	tbl.ApplyMove(0, "multiswap-drawn", url.Values{"slots": {string(rune('0' + a)), string(rune('0' + b))}})

	if _, up := tbl.g.FaceUpCard(0, a); !up {
		t.Fatalf("precondition: slot %d should be face-up after a mismatched multi-swap", a)
	}
	cv := tbl.cardView(0, 0, a)
	if !cv.Show || !cv.FaceUp {
		t.Errorf("hard mode: face-up penalty card must be public (Show=%v FaceUp=%v)", cv.Show, cv.FaceUp)
	}
}

// TestRemoveBot: the admin can add and remove bots in the lobby; a non-admin
// cannot, and a human seat can never be removed.
func TestRemoveBot(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	tbl := NewTable(rng)
	tbl.seats = []seat{{name: "Alice"}}
	tbl.flash = make([]string, 1)
	tbl.phase = Lobby

	tbl.ApplyMove(0, "add-bot", nil)
	tbl.ApplyMove(0, "add-bot", nil)
	if len(tbl.seats) != 3 {
		t.Fatalf("after 2 add-bot, seats=%d want 3", len(tbl.seats))
	}

	// non-admin cannot remove
	tbl.ApplyMove(1, "remove-bot", url.Values{"seat": {"1"}})
	if len(tbl.seats) != 3 {
		t.Errorf("non-admin removed a bot: seats=%d want 3", len(tbl.seats))
	}

	// cannot remove the human seat 0
	tbl.ApplyMove(0, "remove-bot", url.Values{"seat": {"0"}})
	if len(tbl.seats) != 3 || tbl.seats[0].name != "Alice" {
		t.Errorf("human seat was removed: seats=%v", tbl.seats)
	}

	// admin removes bot at index 1
	tbl.ApplyMove(0, "remove-bot", url.Values{"seat": {"1"}})
	if len(tbl.seats) != 2 {
		t.Fatalf("after remove, seats=%d want 2", len(tbl.seats))
	}
	if len(tbl.flash) != 2 {
		t.Errorf("flash not resized with seats: len=%d want 2", len(tbl.flash))
	}
	for _, s := range tbl.seats {
		if s.name == "Alice" {
			continue
		}
		if !s.isBot {
			t.Errorf("remaining non-human seat is not a bot: %+v", s)
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
