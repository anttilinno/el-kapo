// Command kapo is a terminal prototype of the Kapo card game.
package main

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"el-kapo/pkg/ai"
	"el-kapo/pkg/game"
)

func main() {
	r := bufio.NewReader(os.Stdin)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	fmt.Print("Number of AI opponents (1-3): ")
	numAI, err := promptInt(r, "", 1, 3)
	if err != nil {
		fmt.Println("\ngoodbye")
		return
	}

	n := numAI + 1
	names := make([]string, n)
	names[0] = "You"
	for i := 1; i < n; i++ {
		names[i] = fmt.Sprintf("AI-%d", i)
	}
	possessive := make([]string, n)
	possessive[0] = "your"
	for i := 1; i < n; i++ {
		possessive[i] = names[i] + "'s"
	}

	match := game.NewMatch(n)
	starter := -1 // first round: random
	for round := 1; !match.Over(); round++ {
		fmt.Printf("\n=== Round %d ===\n", round)
		next, err := playRound(r, rng, n, names, possessive, match, starter)
		if err != nil {
			fmt.Println("\ngoodbye")
			return
		}
		starter = next
	}

	fmt.Printf("\nMatch over! Winner: %s\n", names[match.Winner()])
	for i, s := range match.Scores {
		fmt.Printf("  %s: %d\n", names[i], s)
	}
}

// playRound plays one round and returns the starting player for the next round
// (the winner, or a random winner on a tie). starter < 0 picks at random.
func playRound(r *bufio.Reader, rng *rand.Rand, n int, names, possessive []string, match *game.Match, starter int) (int, error) {
	var g *game.Game
	if starter < 0 {
		g = game.NewRound(n, rng)
	} else {
		g = game.NewRoundWithStarter(n, starter, rng)
	}
	fmt.Printf("%s starts.\n", names[g.Current()])

	for p := 1; p < n; p++ {
		g.MarkPeek(p, 0)
		g.MarkPeek(p, 1)
	}

	fmt.Println("Peek at 2 of your 4 cards.")
	peeked := map[int]bool{}
	for len(peeked) < 2 {
		slot, err := promptInt(r, "slot to peek (1-4): ", 1, 4)
		if err != nil {
			return -1, err
		}
		slot--
		if peeked[slot] {
			fmt.Println("already peeked that slot")
			continue
		}
		peeked[slot] = true
		fmt.Printf("  slot %d: %s\n", slot+1, g.MarkPeek(0, slot))
	}

	for !g.Ended() {
		cur := g.Current()
		if cur == 0 {
			if err := humanTurn(g, r, names); err != nil {
				return -1, err
			}
		} else {
			for _, l := range ai.Turn(g, cur, rng, possessive) {
				fmt.Printf("%s %s\n", names[cur], l)
			}
		}
	}

	fmt.Println("\n--- Round over, revealing hands ---")
	result := g.Results()
	for p := 0; p < n; p++ {
		fmt.Printf("%s: %v (total %d)\n", names[p], g.Hand(p), result.Totals[p])
	}
	if result.AutoWin >= 0 {
		fmt.Printf("%s auto-wins with 2 queens + 2 kings!\n", names[result.AutoWin])
	} else {
		var wnames []string
		for _, w := range result.Winners {
			wnames = append(wnames, names[w])
		}
		fmt.Printf("Winner(s): %s\n", strings.Join(wnames, ", "))
	}

	match.Apply(result)
	fmt.Println("Match scores:")
	for i, s := range match.Scores {
		fmt.Printf("  %s: %d\n", names[i], s)
	}

	// next round is led by the winner; a random one if the round was tied
	winners := result.Winners
	if result.AutoWin >= 0 {
		winners = []int{result.AutoWin}
	}
	return winners[rng.Intn(len(winners))], nil
}

// ponytail: the human's own known cards are shown persistently below as a
// memory aid for this prototype - the real game requires memorization.
func printState(g *game.Game, names []string) {
	fmt.Println()
	fmt.Println("Your hand:")
	for i := range g.Hand(0) {
		c, known := g.KnownCard(0, i)
		if !known {
			fmt.Printf("  [%d] ?\n", i+1)
			continue
		}
		up := ""
		if _, isUp := g.FaceUpCard(0, i); isUp {
			up = " (face up!)"
		}
		fmt.Printf("  [%d] %s%s\n", i+1, c, up)
	}
	for p := 1; p < g.NumPlayers(); p++ {
		parts := make([]string, len(g.Hand(p)))
		for s := range g.Hand(p) {
			// ponytail: same memory-aid convention as the human's own hand -
			// opponent cards the human has legitimately seen stay visible
			if c, up := g.FaceUpCard(p, s); up {
				parts[s] = c.String() + " up"
			} else if c, ok := g.KnownTo(0, p, s); ok {
				parts[s] = c.String()
			} else {
				parts[s] = "?"
			}
		}
		fmt.Printf("%s: %d cards [%s]\n", names[p], len(parts), strings.Join(parts, ", "))
	}
	if top, ok := g.TopDiscard(); ok {
		fmt.Printf("Discard top: %s\n", top)
	} else {
		fmt.Println("Discard pile: empty")
	}
	fmt.Printf("Deck: %d cards left\n", g.DeckLen())
	if g.KapoCaller() >= 0 {
		fmt.Println("Kapo has been called - this is the final round of turns.")
	}
}

func humanTurn(g *game.Game, r *bufio.Reader, names []string) error {
	printState(g, names)
	for {
		opts := "(d)raw"
		if _, ok := g.TopDiscard(); ok {
			opts += ", (t)ake discard"
		}
		if g.KapoCaller() == -1 {
			opts += ", (k)apo"
		}
		fmt.Printf("%s> ", opts)
		line, err := readLine(r)
		if err != nil {
			return err
		}
		switch strings.ToLower(line) {
		case "d":
			return humanDraw(g, r)
		case "t":
			if _, ok := g.TopDiscard(); !ok {
				fmt.Println("discard pile is empty")
				continue
			}
			return humanTakeDiscard(g, r)
		case "k":
			if err := g.CallKapo(); err != nil {
				fmt.Println(err)
				continue
			}
			fmt.Println("You call Kapo!")
			return nil
		default:
			fmt.Println("invalid choice")
		}
	}
}

// humanTakeDiscard handles the take-discard flow: single-slot swap or multi-swap.
func humanTakeDiscard(g *game.Game, r *bufio.Reader) error {
	max := len(g.Hand(0))
	for {
		fmt.Printf("slot to swap into (1-%d), or (m) multi-swap> ", max)
		line, err := readLine(r)
		if err != nil {
			return err
		}
		if strings.ToLower(line) == "m" {
			slots, err := promptSlots(r, max)
			if err != nil {
				return err
			}
			res, merr := g.MultiSwapDiscard(slots)
			if merr != nil {
				fmt.Println(merr)
				continue
			}
			printMultiSwapResult(res)
			return nil
		}
		slot, convErr := strconv.Atoi(line)
		if convErr != nil || slot < 1 || slot > max {
			fmt.Printf("enter 1-%d or m\n", max)
			continue
		}
		taken, _ := g.TopDiscard()
		old := g.Hand(0)[slot-1]
		if err := g.TakeDiscard(slot - 1); err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Printf("You take the %s into slot %d, discarding the %s.\n", taken, slot, old)
		return nil
	}
}

func humanDraw(g *game.Game, r *bufio.Reader) error {
	drawn, err := g.Draw()
	if err != nil {
		return fmt.Errorf("draw: %w", err)
	}
	fmt.Printf("You drew: %s\n", drawn)
	max := len(g.Hand(0))
	for {
		fmt.Printf("(1-%d) swap into slot, (m) multi-swap, (x) discard> ", max)
		line, err := readLine(r)
		if err != nil {
			return err
		}
		switch strings.ToLower(line) {
		case "x":
			return humanDiscardAction(g, r, drawn)
		case "m":
			slots, err := promptSlots(r, max)
			if err != nil {
				return err
			}
			res, merr := g.MultiSwapDrawn(slots)
			if merr != nil {
				fmt.Println(merr)
				continue
			}
			printMultiSwapResult(res)
			return nil
		}
		slot, convErr := strconv.Atoi(line)
		if convErr != nil || slot < 1 || slot > max {
			fmt.Printf("enter 1-%d, m or x\n", max)
			continue
		}
		old := g.Hand(0)[slot-1]
		if err := g.SwapDrawn(slot - 1); err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Printf("You discard the %s\n", old)
		return nil
	}
}

func printMultiSwapResult(res game.MultiSwapResult) {
	if res.Success {
		fmt.Printf("Multi-swap success! Discarded %s; your hand is now %d cards.\n",
			cardList(res.Cards), res.HandSize)
	} else {
		fmt.Printf("MISMATCH! Those cards stay in your hand and are now FACE UP for everyone: %s. "+
			"The pulled card joins your hand too (now %d cards).\n",
			cardList(res.Cards), res.HandSize)
	}
}

func cardList(cards []game.Card) string {
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = c.String()
	}
	return strings.Join(parts, ", ")
}

func humanDiscardAction(g *game.Game, r *bufio.Reader, drawn game.Card) error {
	at, err := g.DiscardDrawn()
	if err != nil {
		return fmt.Errorf("discard: %w", err)
	}
	fmt.Printf("You discard the %s\n", drawn)

	ownMax := len(g.Hand(0))
	switch at {
	case game.ActionPeekOwn:
		slot, ok, err := promptIntOrSkip(r, fmt.Sprintf("peek own slot (1-%d), or (s) skip> ", ownMax), 1, ownMax)
		if err != nil {
			return err
		}
		if !ok {
			return g.SkipAction()
		}
		c, perr := g.PeekOwn(slot - 1)
		if perr != nil {
			fmt.Println(perr)
			return g.SkipAction()
		}
		fmt.Printf("slot %d is: %s\n", slot, c)

	case game.ActionPeekOther:
		maxOpp := g.NumPlayers() - 1
		opp, ok, err := promptIntOrSkip(r, fmt.Sprintf("peek opponent (1-%d), or (s) skip> ", maxOpp), 1, maxOpp)
		if err != nil {
			return err
		}
		if !ok {
			return g.SkipAction()
		}
		theirMax := len(g.Hand(opp))
		slot, ok, err := promptIntOrSkip(r, fmt.Sprintf("their slot (1-%d), or (s) skip> ", theirMax), 1, theirMax)
		if err != nil {
			return err
		}
		if !ok {
			return g.SkipAction()
		}
		c, perr := g.PeekOther(opp, slot-1)
		if perr != nil {
			fmt.Println(perr)
			return g.SkipAction()
		}
		fmt.Printf("AI-%d's slot %d is: %s\n", opp, slot, c)

	case game.ActionSwapOther:
		own, ok, err := promptIntOrSkip(r, fmt.Sprintf("your slot to swap (1-%d), or (s) skip> ", ownMax), 1, ownMax)
		if err != nil {
			return err
		}
		if !ok {
			return g.SkipAction()
		}
		maxOpp := g.NumPlayers() - 1
		opp, ok, err := promptIntOrSkip(r, fmt.Sprintf("opponent (1-%d), or (s) skip> ", maxOpp), 1, maxOpp)
		if err != nil {
			return err
		}
		if !ok {
			return g.SkipAction()
		}
		theirMax := len(g.Hand(opp))
		theirSlot, ok, err := promptIntOrSkip(r, fmt.Sprintf("their slot (1-%d), or (s) skip> ", theirMax), 1, theirMax)
		if err != nil {
			return err
		}
		if !ok {
			return g.SkipAction()
		}
		if serr := g.SwapOther(own-1, opp, theirSlot-1); serr != nil {
			fmt.Println(serr)
			return g.SkipAction()
		}
		fmt.Println("Swapped blind.")
	}
	return nil
}

// readLine reads one line of input, trimmed. It returns io.EOF once the
// input stream is exhausted and there is no more content to process.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	trimmed := strings.TrimSpace(line)
	if err != nil {
		if trimmed != "" {
			return trimmed, nil
		}
		return "", io.EOF
	}
	return trimmed, nil
}

func promptInt(r *bufio.Reader, prompt string, lo, hi int) (int, error) {
	for {
		if prompt != "" {
			fmt.Print(prompt)
		}
		line, err := readLine(r)
		if err != nil {
			return 0, err
		}
		n, convErr := strconv.Atoi(line)
		if convErr != nil || n < lo || n > hi {
			fmt.Printf("enter a number %d-%d\n", lo, hi)
			continue
		}
		return n, nil
	}
}

// promptSlots reads 2+ space-separated 1-based slot numbers, returned 0-based.
func promptSlots(r *bufio.Reader, max int) ([]int, error) {
	for {
		fmt.Print("slots to multi-swap, space-separated (e.g. \"1 3\"): ")
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(line)
		slots := make([]int, 0, len(fields))
		ok := len(fields) >= 2
		for _, f := range fields {
			n, convErr := strconv.Atoi(f)
			if convErr != nil || n < 1 || n > max {
				ok = false
				break
			}
			slots = append(slots, n-1)
		}
		if !ok {
			fmt.Printf("enter 2 or more slot numbers between 1 and %d\n", max)
			continue
		}
		return slots, nil
	}
}

// promptIntOrSkip is like promptInt but "s" returns ok=false instead of a number.
func promptIntOrSkip(r *bufio.Reader, prompt string, lo, hi int) (n int, ok bool, err error) {
	for {
		fmt.Print(prompt)
		line, err := readLine(r)
		if err != nil {
			return 0, false, err
		}
		if strings.ToLower(line) == "s" {
			return 0, false, nil
		}
		n, convErr := strconv.Atoi(line)
		if convErr != nil || n < lo || n > hi {
			fmt.Printf("enter %d-%d or s\n", lo, hi)
			continue
		}
		return n, true, nil
	}
}
