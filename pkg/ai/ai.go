// Package ai implements a heuristic Kapo opponent.
package ai

import (
	"fmt"
	"math/rand"
	"strings"

	"el-kapo/pkg/game"
)

// Tuning knobs for the heuristic.
const (
	kapoThresholdEarly = 8  // call kapo at or below this while deck >50% full
	kapoThresholdLate  = 5  // tighter once <50% left: low cards are scooped, winning hands run lower
	unknownCardValue   = 7  // assumed value of an unknown slot when estimating
	takeDiscardMargin  = 1  // only take discard if it beats our target by more than this
	swapDrawnLowValue  = 4  // swap a low drawn card into an unknown slot below this value
	swapOtherThreshold = 10 // blind-swap our worst known card away if it's at least this bad
	peekOverSwapMargin = 4  // prefer scouting with a drawn 7/8 unless swapping it in saves this much
)

// Turn plays one AI turn on g and returns narration lines for the CLI to
// print. Lines never reveal the AI's private card values - only public
// information (cards that land face-up on the discard pile, face-up hand
// cards) is named. The AI never gambles on multi-swaps: it only selects
// slots it knows are the same rank, so its multi-swaps always succeed.
// names are possessive display names indexed by player ("your", "AI-1's").
func Turn(g *game.Game, me int, rng *rand.Rand, names []string) []string {
	if estimateHand(g, me) <= kapoThreshold(g) && g.KapoCaller() == -1 {
		_ = g.CallKapo()
		return []string{"calls Kapo!"}
	}

	group, groupSum, hasGroup := sameRankGroup(g, me)
	maxSlot, maxVal, haveKnown := maxKnownSlot(g, me)
	top, hasTop := g.TopDiscard()

	// Grow an existing same-rank group instead of consuming it. If the discard
	// matches the group's rank and we hold a worse known card outside the group,
	// take the discard onto that slot: e.g. 4,4,5 + discard 4 -> take onto the 5
	// for 4,4,4, then multi-swap all three next turn. That sheds two cards where
	// multi-swapping the pair now for a single 4 only sheds one.
	if hasTop && hasGroup {
		if gr, ok := g.KnownCard(me, group[0]); ok && top.Rank == gr.Rank {
			if slot, ok := maxKnownOutsideGroup(g, me, group, top.Points()); ok {
				old := g.Hand(me)[slot]
				_ = g.TakeDiscard(slot)
				return []string{fmt.Sprintf("takes the %s from the discard pile into slot %d, discarding the %s", top, slot+1, old)}
			}
		}
	}

	if hasTop && hasGroup && top.Points() < groupSum-takeDiscardMargin {
		res, _ := g.MultiSwapDiscard(group)
		return []string{fmt.Sprintf("multi-swaps slots %s (%s) for the %s from the discard pile",
			slotList(group), cardList(res.Cards), top)}
	}

	if hasTop && haveKnown && top.Points() < maxVal-takeDiscardMargin {
		old := g.Hand(me)[maxSlot]
		_ = g.TakeDiscard(maxSlot)
		return []string{fmt.Sprintf("takes the %s from the discard pile into slot %d, discarding the %s", top, maxSlot+1, old)}
	}

	// a low discard also beats the expected value of an unknown slot
	if slot, ok := firstUnknownSlot(g, me); hasTop && ok && top.Points() < unknownCardValue-takeDiscardMargin {
		old := g.Hand(me)[slot]
		_ = g.TakeDiscard(slot)
		return []string{fmt.Sprintf("takes the %s from the discard pile into slot %d, discarding the %s", top, slot+1, old)}
	}

	drawn, err := g.Draw()
	if err != nil {
		return []string{"has no legal move"}
	}

	// multi-swap beats single swap when it removes a whole same-rank group
	if hasGroup && drawn.Points() < groupSum {
		res, _ := g.MultiSwapDrawn(group)
		return []string{fmt.Sprintf("multi-swaps slots %s (%s) for the drawn card",
			slotList(group), cardList(res.Cards))}
	}

	// a drawn 7/8 is worth more as a scout of our own unknown slots than as a
	// small hand upgrade - only swap it in when the points win is big
	_, hasUnknown := firstUnknownSlot(g, me)
	scoutInstead := (drawn.Rank == 7 || drawn.Rank == 8) && hasUnknown &&
		maxVal-drawn.Points() < peekOverSwapMargin

	if !scoutInstead && haveKnown && drawn.Points() < maxVal {
		old := g.Hand(me)[maxSlot]
		_ = g.SwapDrawn(maxSlot)
		return []string{fmt.Sprintf("swaps a drawn card into slot %d, discarding the %s", maxSlot+1, old)}
	}

	if slot, ok := firstUnknownSlot(g, me); ok && !scoutInstead && drawn.Points() <= swapDrawnLowValue {
		old := g.Hand(me)[slot]
		_ = g.SwapDrawn(slot)
		return []string{fmt.Sprintf("swaps a drawn card into slot %d, discarding the %s", slot+1, old)}
	}

	at, _ := g.DiscardDrawn()
	lines := []string{fmt.Sprintf("discards the %s", drawn)}

	switch at {
	case game.ActionPeekOwn:
		if slot, ok := firstUnknownSlot(g, me); ok {
			_, _ = g.PeekOwn(slot)
			lines = append(lines, fmt.Sprintf("peeks at its own slot %d", slot+1))
		} else {
			_ = g.SkipAction()
		}
	case game.ActionPeekOther:
		opp := randomOpponent(g, me, rng)
		slot := rng.Intn(len(g.Hand(opp)))
		_, _ = g.PeekOther(opp, slot)
		lines = append(lines, fmt.Sprintf("peeks at %s slot %d", names[opp], slot+1))
	case game.ActionSwapOther:
		if caller := g.KapoCaller(); caller >= 0 && caller != me && haveKnown {
			// desperate move: dump our highest known card on the kapo caller,
			// stealing their lowest card we know of (or a random one)
			slot, known := lowestKnownSlotOf(g, me, caller)
			if !known {
				slot = rng.Intn(len(g.Hand(caller)))
			}
			_ = g.SwapOther(maxSlot, caller, slot)
			lines = append(lines, fmt.Sprintf("desperately swaps its slot %d onto kapo caller %s slot %d", maxSlot+1, names[caller], slot+1))
		} else if opp, slot, c, ok := bestKnownTarget(g, me, maxVal); ok && haveKnown {
			// only name the target card if it is public (face-up); a card
			// known via a private peek must not be revealed in narration
			_, isUp := g.FaceUpCard(opp, slot)
			_ = g.SwapOther(maxSlot, opp, slot)
			if isUp {
				lines = append(lines, fmt.Sprintf("blind-swaps its slot %d for the face-up %s in %s slot %d", maxSlot+1, c, names[opp], slot+1))
			} else {
				lines = append(lines, fmt.Sprintf("blind-swaps its slot %d with %s slot %d", maxSlot+1, names[opp], slot+1))
			}
		} else if haveKnown && maxVal >= swapOtherThreshold {
			opp := randomOpponent(g, me, rng)
			slot := rng.Intn(len(g.Hand(opp)))
			_ = g.SwapOther(maxSlot, opp, slot)
			lines = append(lines, fmt.Sprintf("blind-swaps its slot %d with %s slot %d", maxSlot+1, names[opp], slot+1))
		} else {
			_ = g.SkipAction()
		}
	}

	return lines
}

// kapoThreshold scales boldness with deck depletion: early on an 8-point hand
// is worth calling, late game the low cards are already scooped up.
func kapoThreshold(g *game.Game) int {
	initial := 52 - 4*g.NumPlayers()
	if g.DeckLen()*2 > initial {
		return kapoThresholdEarly
	}
	return kapoThresholdLate
}

func estimateHand(g *game.Game, me int) int {
	total := 0
	for s := range g.Hand(me) {
		if c, ok := g.KnownCard(me, s); ok {
			total += c.Points()
		} else {
			total += unknownCardValue
		}
	}
	return total
}

// sameRankGroup returns the known own slots forming the highest-total group
// of 2+ same-rank cards, if any.
func sameRankGroup(g *game.Game, me int) (slots []int, sum int, ok bool) {
	byRank := map[game.Rank][]int{}
	for s := range g.Hand(me) {
		if c, known := g.KnownCard(me, s); known {
			byRank[c.Rank] = append(byRank[c.Rank], s)
		}
	}
	for r, group := range byRank {
		if len(group) >= 2 && int(r)*len(group) > sum {
			slots, sum = group, int(r)*len(group)
		}
	}
	return slots, sum, slots != nil
}

// maxKnownSlot returns the highest-value known own slot, if any.
func maxKnownSlot(g *game.Game, me int) (slot, val int, ok bool) {
	slot, val = -1, -1
	for s := range g.Hand(me) {
		if c, known := g.KnownCard(me, s); known && c.Points() > val {
			slot, val = s, c.Points()
		}
	}
	return slot, val, slot >= 0
}

// maxKnownOutsideGroup returns the highest-value known own slot NOT in group
// whose value exceeds minVal, so replacing it with an equal-rank discard both
// improves the hand and extends the group for a bigger later multi-swap.
func maxKnownOutsideGroup(g *game.Game, me int, group []int, minVal int) (slot int, ok bool) {
	inGroup := make(map[int]bool, len(group))
	for _, s := range group {
		inGroup[s] = true
	}
	slot, best := -1, minVal
	for s := range g.Hand(me) {
		if inGroup[s] {
			continue
		}
		if c, known := g.KnownCard(me, s); known && c.Points() > best {
			slot, best = s, c.Points()
		}
	}
	return slot, slot >= 0
}

func firstUnknownSlot(g *game.Game, me int) (int, bool) {
	for s := range g.Hand(me) {
		if _, ok := g.KnownCard(me, s); !ok {
			return s, true
		}
	}
	return -1, false
}

// bestKnownTarget finds the lowest-point opponent card the AI knows (face-up
// or legitimately seen via peeks/public moves) worth less than our max known
// own card.
func bestKnownTarget(g *game.Game, me, maxVal int) (opp, slot int, c game.Card, ok bool) {
	best := maxVal
	for p := 0; p < g.NumPlayers(); p++ {
		if p == me {
			continue
		}
		for s := range g.Hand(p) {
			if kc, known := g.KnownTo(me, p, s); known && kc.Points() < best {
				opp, slot, c, ok = p, s, kc, true
				best = kc.Points()
			}
		}
	}
	return opp, slot, c, ok
}

// lowestKnownSlotOf returns p's lowest-point slot that viewer me knows, if any.
func lowestKnownSlotOf(g *game.Game, me, p int) (int, bool) {
	slot, best := -1, 0
	for s := range g.Hand(p) {
		if c, known := g.KnownTo(me, p, s); known && (slot < 0 || c.Points() < best) {
			slot, best = s, c.Points()
		}
	}
	return slot, slot >= 0
}

func randomOpponent(g *game.Game, me int, rng *rand.Rand) int {
	n := g.NumPlayers()
	for {
		if p := rng.Intn(n); p != me {
			return p
		}
	}
}

// slotList formats 0-based slots as 1-based prose, e.g. "2 and 4".
func slotList(slots []int) string {
	parts := make([]string, len(slots))
	for i, s := range slots {
		parts[i] = fmt.Sprintf("%d", s+1)
	}
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return parts[0]
}

func cardList(cards []game.Card) string {
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = c.String()
	}
	return strings.Join(parts, ", ")
}
