package server

import (
	"fmt"
	"math/rand"
	"net/url"
	"testing"
	"time"

	"el-kapo/pkg/game"
)

// TestSimFullRounds drives many full 1-human/1-bot rounds with a random but
// legal human policy, asserting the table always makes progress (never gets
// stuck with a bot to move and no runner advancing the game). Reproduction
// harness for a live hang seen after a human Kapo call.
func TestSimFullRounds(t *testing.T) {
	if testing.Short() {
		t.Skip("slow bot-pacing sim; skipped with -short")
	}
	for seed := int64(1); seed <= 10; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			tbl := NewTable(rng)
			tbl.seats = []seat{{name: "H"}, {name: "B", isBot: true}}
			tbl.flash = make([]string, 2)
			tbl.match = game.NewMatch(2)
			tbl.startRound(0)

			tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"0"}})
			tbl.ApplyMove(0, "peek-setup", url.Values{"slot": {"1"}})

			hrng := rand.New(rand.NewSource(seed * 77))
			deadline := time.Now().Add(10 * time.Second)
			moves := 0
			for time.Now().Before(deadline) {
				switch tbl.Phase() {
				case RoundOver, MatchOver:
					return // round completed - success
				case Playing:
				default:
					t.Fatalf("unexpected phase %v", tbl.Phase())
				}

				tbl.mu.Lock()
				myTurn := tbl.g.Current() == 0
				gphase := tbl.g.Phase()
				handLen := len(tbl.g.Hand(0))
				kapoCalled := tbl.g.KapoCaller() != -1
				tbl.mu.Unlock()

				if !myTurn {
					time.Sleep(20 * time.Millisecond)
					continue
				}
				moves++
				if moves > 500 {
					t.Fatalf("no round end after %d human moves", moves)
				}

				switch gphase {
				case game.TurnStart:
					if !kapoCalled && hrng.Intn(4) == 0 {
						tbl.ApplyMove(0, "kapo", nil)
					} else {
						tbl.ApplyMove(0, "draw", nil)
					}
				case game.Drawn:
					if hrng.Intn(2) == 0 {
						tbl.ApplyMove(0, "discard", nil)
					} else {
						slot := fmt.Sprint(hrng.Intn(handLen))
						tbl.ApplyMove(0, "swap", url.Values{"slot": {slot}})
					}
				case game.Action:
					tbl.ApplyMove(0, "skip", nil)
				}
			}
			tbl.mu.Lock()
			cur := tbl.g.Current()
			gp := tbl.g.Phase()
			tbl.mu.Unlock()
			t.Fatalf("stuck: table phase=%v current=%d game phase=%v", tbl.Phase(), cur, gp)
		})
	}
}
