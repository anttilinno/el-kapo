// Package server hosts the browser multiplayer UI for Kapo: an HTTP+SSE
// front end over the pure pkg/game engine.
package server

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"el-kapo/pkg/ai"
	"el-kapo/pkg/game"
)

// TablePhase is the table-level state machine, one level above game.Phase
// (which only tracks where the current player is within a single turn).
type TablePhase int

const (
	Lobby     TablePhase = iota // seats unfilled, waiting for humans to join
	Peeking                     // humans picking their 2 setup peeks
	Playing                     // a game.Game round is in progress
	RoundOver                   // results visible, waiting for next-round click
	MatchOver                   // match.Over(), waiting for a new game
	Ended                       // admin ended the game early
)

// seat is one place at the table: a human (identified by sid cookie) or a bot.
type seat struct {
	name      string
	sid       string // "" for bots, and for humans who haven't joined yet
	isBot     bool
	peekSlots []int // slots marked during Peeking, in click order
}

// subscriber is one SSE connection watching the table from a given seat's
// point of view (views differ - each seat has different private knowledge).
type subscriber struct {
	seat int
	ch   chan string
}

// Table is a single running (or waiting) game, one HTTP-visible URL.
//
// ponytail: in-memory tables, no GC - add a TTL sweep in the registry if
// this ever needs to survive a restart or run for the public with many
// abandoned tables.
type Table struct {
	mu sync.Mutex

	id    string
	phase TablePhase
	seats []seat

	g     *game.Game
	match *game.Match
	round int

	lastResult  game.RoundResult
	nextStarter int // seat to start the next round; -1 = random

	drawnCard     game.Card       // current player's undecided drawn card
	pendingAction game.ActionType // action unlocked by the last DiscardDrawn
	armedSlot     int             // own slot armed for a pending swap-other; -1 = none
	armedPlayer   int             // opponent seat armed for swap-other; -1 = none
	armedTheir    int             // opponent slot armed for swap-other; -1 = none

	flash []string // per-seat private message, cleared on that seat's next move
	log   []string // shared public event log, newest last

	// Hard mode: peeked cards are only shown for revealFor, then rendered
	// face-down again - the player has to memorize them. Only the intentional
	// peeks (starting pair, 7/8 own, 9/10 opponent) grant a reveal; swaps and
	// takes stay face-down. reveals holds the show-until deadline per
	// (viewer, player, slot).
	hard    bool
	reveals map[revealKey]time.Time

	subs      []subscriber
	botLoopOn bool // the per-table bot loop goroutine has been started

	rng *rand.Rand
}

// revealKey identifies one viewer's sight of one card slot.
type revealKey struct{ viewer, player, slot int }

// revealFor is how long a newly revealed card stays visible in hard mode.
const revealFor = 3 * time.Second

// botTick paces bot turns so humans can follow them on the SSE stream. ~2s
// matches the per-card cadence common in digital card games (tablet clients
// run ~2s/card; deliberate AI up to ~5s). Var so tests can drop it to run fast.
var botTick = 2 * time.Second

// NewTable creates an empty table with a random 16-hex-char id.
func NewTable(rng *rand.Rand) *Table {
	return &Table{id: randHex(8), rng: rng, nextStarter: -1, armedSlot: -1, armedPlayer: -1, armedTheir: -1}
}

func (t *Table) ID() string { return t.id }

// Phase reports the table's current phase.
func (t *Table) Phase() TablePhase {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.phase
}

const maxSeats = 4

// Init seats the creator alone at seat 0 and opens the lobby. Bots and other
// humans are added live from the seats screen; the admin deals when ready.
func (t *Table) Init(creatorName, creatorSID string, hard bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.hard = hard
	t.seats = []seat{{name: creatorName, sid: creatorSID}}
	t.flash = make([]string, 1)
	t.phase = Lobby
}

// Join appends a new human seat for sid while the lobby is open and has room.
// Unlike the old fixed-seat model it never auto-starts; the admin deals.
func (t *Table) Join(sid, name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.phase != Lobby {
		return fmt.Errorf("table is not accepting joins")
	}
	if len(t.seats) >= maxSeats {
		return fmt.Errorf("table is full")
	}
	t.seats = append(t.seats, seat{name: name, sid: sid})
	t.flash = append(t.flash, "")
	return nil
}

// SeatForSID returns the seat index claimed by sid, if any.
func (t *Table) SeatForSID(sid string) (int, bool) {
	if sid == "" {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, s := range t.seats {
		if !s.isBot && s.sid == sid {
			return i, true
		}
	}
	return 0, false
}

// FreeHumanSeat reports whether a newcomer can still join: the lobby is open
// and under the seat cap.
func (t *Table) FreeHumanSeat() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase == Lobby && len(t.seats) < maxSeats {
		return len(t.seats), true
	}
	return 0, false
}

// startRound deals a fresh game.Game, resets per-round bookkeeping, has bots
// take their setup peeks immediately, and enters Peeking (or straight into
// Playing if every seat is a bot's or already peeked, which in practice only
// happens if... it never does, since there's always >=1 human, but the check
// is harmless). Caller must hold t.mu.
func (t *Table) startRound(starter int) {
	n := len(t.seats)
	if starter < 0 {
		t.g = game.NewRound(n, t.rng)
	} else {
		t.g = game.NewRoundWithStarter(n, starter, t.rng)
	}
	t.round++
	t.pendingAction = game.ActionNone
	t.drawnCard = game.Card{}
	t.armedSlot, t.armedPlayer, t.armedTheir = -1, -1, -1
	t.flash = make([]string, n)
	t.phase = Peeking
	t.reveals = make(map[revealKey]time.Time)

	for i := range t.seats {
		t.seats[i].peekSlots = nil
		if t.seats[i].isBot {
			t.g.MarkPeek(i, 0)
			t.g.MarkPeek(i, 1)
			t.seats[i].peekSlots = []int{0, 1}
		}
	}
	t.logf("Round %d begins, %s starts", t.round, t.seats[t.g.Current()].name)

	if t.allHumansPeeked() {
		t.phase = Playing
	}
	if !t.botLoopOn {
		t.botLoopOn = true
		go t.botLoop()
	}
}

func (t *Table) allHumansPeeked() bool {
	for _, s := range t.seats {
		if !s.isBot && len(s.peekSlots) < 2 {
			return false
		}
	}
	return true
}

// ApplyMove validates and applies one player action. Engine/validation
// errors are stashed as a private flash for the acting seat rather than
// returned - the caller (HTTP handler) always just re-broadcasts afterward.
func (t *Table) ApplyMove(seat int, action string, form url.Values) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if seat < 0 || seat >= len(t.seats) || t.seats[seat].isBot {
		return
	}
	t.flash[seat] = ""

	switch action {
	case "peek-setup":
		t.doPeekSetup(seat, form)
	case "add-bot":
		t.doAddBot(seat)
	case "remove-bot":
		t.doRemoveBot(seat, form)
	case "set-difficulty":
		t.doSetDifficulty(seat, form)
	case "deal":
		t.doDeal(seat)
	case "next-round":
		t.doNextRound(seat)
	case "end-game":
		t.doEndGame(seat)
	default:
		if t.phase != Playing || seat != t.g.Current() {
			t.flash[seat] = "not your turn"
			return
		}
		t.doTurnAction(seat, action, form)
	}
}

// reveal grants viewer a timed look at player's slot in hard mode and
// schedules a re-broadcast once it expires, so the board re-renders the card
// face-down again. Caller must hold t.mu. Easy mode: no-op, reveals are
// permanent.
func (t *Table) reveal(viewer, player, slot int) {
	if !t.hard {
		return
	}
	t.reveals[revealKey{viewer, player, slot}] = time.Now().Add(revealFor)
	time.AfterFunc(revealFor+100*time.Millisecond, t.Broadcast)
}

// revealActive reports whether viewer may currently see player's slot under
// hard-mode rules. Easy mode: always. Caller must hold t.mu.
func (t *Table) revealActive(viewer, player, slot int) bool {
	if !t.hard {
		return true
	}
	d, ok := t.reveals[revealKey{viewer, player, slot}]
	return ok && time.Now().Before(d)
}

func (t *Table) doPeekSetup(seat int, form url.Values) {
	if t.phase != Peeking {
		t.flash[seat] = "not peeking now"
		return
	}
	if len(t.seats[seat].peekSlots) >= 2 {
		t.flash[seat] = "already peeked twice"
		return
	}
	slot, err := slotArg(form, "slot")
	if err != nil {
		t.flash[seat] = err.Error()
		return
	}
	for _, s := range t.seats[seat].peekSlots {
		if s == slot {
			t.flash[seat] = "already peeked that slot"
			return
		}
	}
	t.g.MarkPeek(seat, slot)
	t.seats[seat].peekSlots = append(t.seats[seat].peekSlots, slot)
	t.reveal(seat, seat, slot)

	if t.allHumansPeeked() {
		t.phase = Playing
	}
}

// doAddBot appends a bot seat while the lobby is open and under the cap.
// Admin-only, since the seats screen only shows the button to the creator.
func (t *Table) doAddBot(by int) {
	if by != 0 {
		t.flash[by] = "only the admin can add bots"
		return
	}
	if t.phase != Lobby {
		return
	}
	if len(t.seats) >= maxSeats {
		t.flash[by] = "table is full"
		return
	}
	bots := 0
	for _, s := range t.seats {
		if s.isBot {
			bots++
		}
	}
	t.seats = append(t.seats, seat{name: fmt.Sprintf("AI-%d", bots+1), isBot: true})
	t.flash = append(t.flash, "")
}

// doRemoveBot removes the bot seat at the given index while the lobby is open.
// Admin-only; refuses to remove a human seat.
func (t *Table) doRemoveBot(by int, form url.Values) {
	if by != 0 {
		t.flash[by] = "only the admin can remove bots"
		return
	}
	if t.phase != Lobby {
		return
	}
	idx, err := slotArg(form, "seat")
	if err != nil || idx < 0 || idx >= len(t.seats) || !t.seats[idx].isBot {
		return
	}
	t.seats = append(t.seats[:idx], t.seats[idx+1:]...)
	t.flash = append(t.flash[:idx], t.flash[idx+1:]...)
}

// doSetDifficulty flips easy/hard from the lobby segmented control (admin-only).
func (t *Table) doSetDifficulty(seat int, form url.Values) {
	if seat != 0 {
		t.flash[seat] = "only the admin can change difficulty"
		return
	}
	if t.phase != Lobby {
		return
	}
	t.hard = form.Get("difficulty") == "hard"
}

// doDeal starts the first round once at least two seats are filled (admin-only).
func (t *Table) doDeal(seat int) {
	if seat != 0 {
		t.flash[seat] = "only the admin can deal"
		return
	}
	if t.phase != Lobby {
		return
	}
	if len(t.seats) < 2 {
		t.flash[seat] = "need at least 2 players"
		return
	}
	t.match = game.NewMatch(len(t.seats))
	t.startRound(-1)
}

// doEndGame lets the table admin (seat 0, the creator) end the game from any
// phase. Bots stop on their next wake since the phase is no longer Playing.
func (t *Table) doEndGame(seat int) {
	if seat != 0 {
		t.flash[seat] = "only the admin can end the game"
		return
	}
	if t.phase == Ended {
		return
	}
	t.phase = Ended
	t.logf("%s (admin) ended the game", t.seats[0].name)
}

func (t *Table) doNextRound(seat int) {
	if t.phase != RoundOver {
		t.flash[seat] = "round isn't over yet"
		return
	}
	t.startRound(t.nextStarter)
}

// doTurnAction dispatches the actions only the current player may take.
// Caller has already checked t.phase == Playing && seat == t.g.Current().
func (t *Table) doTurnAction(seat int, action string, form url.Values) {
	name := t.seats[seat].name
	var err error

	switch action {
	case "draw":
		var c game.Card
		c, err = t.g.Draw()
		if err == nil {
			t.drawnCard = c
		}

	case "take":
		var slot int
		if slot, err = slotArg(form, "slot"); err != nil {
			break
		}
		top, _ := t.g.TopDiscard()
		if err = t.g.TakeDiscard(slot); err == nil {
			t.logf("%s takes the %s from the discard pile into slot %d", name, top, slot+1)
		}

	case "swap":
		var slot int
		if slot, err = slotArg(form, "slot"); err != nil {
			break
		}
		old := t.g.Hand(seat)[slot]
		if err = t.g.SwapDrawn(slot); err == nil {
			t.logf("%s discards the %s", name, old)
		}

	case "discard":
		var at game.ActionType
		drawn := t.drawnCard
		if at, err = t.g.DiscardDrawn(); err == nil {
			t.logf("%s discards the %s", name, drawn)
			t.pendingAction = at
			t.armedSlot, t.armedPlayer, t.armedTheir = -1, -1, -1
		}

	case "multiswap-drawn":
		var slots []int
		if slots, err = slotsArg(form); err != nil {
			break
		}
		var res game.MultiSwapResult
		if res, err = t.g.MultiSwapDrawn(slots); err == nil {
			t.logMultiSwap(seat, slots, res, "the drawn card")
		}

	case "multiswap-discard":
		var slots []int
		if slots, err = slotsArg(form); err != nil {
			break
		}
		top, _ := t.g.TopDiscard()
		var res game.MultiSwapResult
		if res, err = t.g.MultiSwapDiscard(slots); err == nil {
			t.logMultiSwap(seat, slots, res, fmt.Sprintf("the %s from the discard pile", top))
		}

	case "peekown":
		var slot int
		if slot, err = slotArg(form, "slot"); err != nil {
			break
		}
		if _, err = t.g.PeekOwn(slot); err == nil {
			t.reveal(seat, seat, slot)
			t.pendingAction = game.ActionNone
		}

	case "peekother":
		var p, s int
		if p, s, err = targetArg(form); err != nil {
			break
		}
		if _, err = t.g.PeekOther(p, s); err == nil {
			t.reveal(seat, p, s)
			t.pendingAction = game.ActionNone
		}

	case "arm-swap":
		if t.pendingAction != game.ActionSwapOther {
			err = fmt.Errorf("no swap pending")
			break
		}
		var slot int
		if slot, err = slotArg(form, "slot"); err != nil {
			break
		}
		if t.armedPlayer >= 0 {
			// opponent card already picked — this own card completes the swap
			if err = t.g.SwapOther(slot, t.armedPlayer, t.armedTheir); err == nil {
				t.logf("%s blind-swaps a card with %s", name, t.seats[t.armedPlayer].name)
				t.pendingAction = game.ActionNone
				t.armedSlot, t.armedPlayer, t.armedTheir = -1, -1, -1
			}
		} else {
			t.armedSlot = slot
		}

	case "swapother":
		if t.pendingAction != game.ActionSwapOther {
			err = fmt.Errorf("no swap pending")
			break
		}
		var p, s int
		if p, s, err = targetArg(form); err != nil {
			break
		}
		if t.armedSlot >= 0 {
			// own card already picked — this opponent card completes the swap
			if err = t.g.SwapOther(t.armedSlot, p, s); err == nil {
				t.logf("%s blind-swaps a card with %s", name, t.seats[p].name)
				t.pendingAction = game.ActionNone
				t.armedSlot, t.armedPlayer, t.armedTheir = -1, -1, -1
			}
		} else {
			t.armedPlayer, t.armedTheir = p, s
		}

	case "skip":
		if err = t.g.SkipAction(); err == nil {
			t.pendingAction = game.ActionNone
			t.armedSlot, t.armedPlayer, t.armedTheir = -1, -1, -1
		}

	case "kapo":
		if err = t.g.CallKapo(); err == nil {
			t.logf("%s calls Kapo!", name)
		}

	default:
		err = fmt.Errorf("unknown action %q", action)
	}

	if err != nil {
		t.flash[seat] = err.Error()
		return
	}
	t.checkRoundEnd()
}

func (t *Table) logMultiSwap(seat int, slots []int, res game.MultiSwapResult, source string) {
	name := t.seats[seat].name
	if res.Success {
		t.logf("%s multi-swaps slots %s (%s) for %s", name, slotList(slots), cardList(res.Cards), source)
	} else {
		t.logf("%s attempts a multi-swap on slots %s - MISMATCH, now face up: %s",
			name, slotList(slots), cardList(res.Cards))
	}
}

// checkRoundEnd transitions RoundOver/MatchOver once the engine reports the
// round has ended. Caller must hold t.mu.
func (t *Table) checkRoundEnd() {
	if t.g == nil || !t.g.Ended() {
		return
	}
	result := t.g.Results()
	t.match.Apply(result)
	t.lastResult = result

	winners := result.Winners
	if result.AutoWin >= 0 {
		winners = []int{result.AutoWin}
		t.logf("%s auto-wins the round with 2 queens and 2 kings!", t.seats[result.AutoWin].name)
	} else {
		names := make([]string, len(winners))
		for i, w := range winners {
			names[i] = t.seats[w].name
		}
		t.logf("Round %d over - winner(s): %s", t.round, strings.Join(names, ", "))
	}
	t.nextStarter = winners[t.rng.Intn(len(winners))]

	if t.match.Over() {
		t.phase = MatchOver
	} else {
		t.phase = RoundOver
	}
}

// botLoop is the table's single bot driver, started once per table and alive
// until the table reaches a terminal phase. Each tick it plays exactly one
// bot turn if one is due; the polling design has no start/stop bookkeeping to
// get wrong, so a bot turn can never be silently dropped. The tick doubles as
// pacing so humans can follow bot moves on the SSE stream.
func (t *Table) botLoop() {
	for {
		// Paces bot turns slow enough for a human to follow each move on the
		// SSE stream - a whole bot turn lands in one broadcast, so too fast a
		// tick makes opponents' moves impossible to catch. Var, not const, so
		// tests can run it fast.
		time.Sleep(botTick)

		t.mu.Lock()
		if t.phase == Ended || t.phase == MatchOver {
			t.botLoopOn = false
			t.mu.Unlock()
			return
		}
		acted := t.phase == Playing && t.g != nil && t.seats[t.g.Current()].isBot
		if acted {
			cur := t.g.Current()
			names := t.possessiveNames()
			for _, line := range ai.Turn(t.g, cur, t.rng, names) {
				t.logf("%s %s", t.seats[cur].name, line)
			}
			t.checkRoundEnd()
		}
		t.mu.Unlock()

		if acted {
			t.Broadcast()
		}
	}
}

// possessiveNames builds the display-name slice ai.Turn narrates with.
func (t *Table) possessiveNames() []string {
	names := make([]string, len(t.seats))
	for i, s := range t.seats {
		names[i] = s.name + "'s"
	}
	return names
}

func (t *Table) logf(format string, args ...any) {
	t.log = append(t.log, fmt.Sprintf(format, args...))
	const maxLog = 30
	if len(t.log) > maxLog {
		t.log = t.log[len(t.log)-maxLog:]
	}
}

// Subscribe registers an SSE listener for seat and returns its channel plus
// a cancel func to unregister and close it.
func (t *Table) Subscribe(seat int) (<-chan string, func()) {
	ch := make(chan string, 8)
	t.mu.Lock()
	t.subs = append(t.subs, subscriber{seat, ch})
	t.mu.Unlock()

	cancel := func() {
		t.mu.Lock()
		for i, s := range t.subs {
			if s.ch == ch {
				t.subs = append(t.subs[:i], t.subs[i+1:]...)
				break
			}
		}
		t.mu.Unlock()
	}
	return ch, cancel
}

// Broadcast re-renders the board for every subscribed seat and pushes it,
// non-blocking (a full buffer just drops the update - the next one will
// carry current state anyway).
func (t *Table) Broadcast() {
	t.mu.Lock()
	subs := append([]subscriber(nil), t.subs...)
	t.mu.Unlock()

	cache := map[int]string{}
	for _, s := range subs {
		frag, ok := cache[s.seat]
		if !ok {
			frag = t.RenderFragment(s.seat)
			cache[s.seat] = frag
		}
		select {
		case s.ch <- frag:
		default:
		}
	}
}

// RenderFragment renders the board_frag template for the given viewer seat.
func (t *Table) RenderFragment(seat int) string {
	view := t.View(seat)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "board_frag.html", view); err != nil {
		return fmt.Sprintf("<p>render error: %s</p>", err)
	}
	return buf.String()
}

// View builds the per-viewer BoardView under lock.
func (t *Table) View(seat int) BoardView {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buildView(seat)
}

// randHex returns n random bytes hex-encoded, used for table ids and the sid
// cookie. Falls back to a time-derived value in the astronomically unlikely
// case crypto/rand fails.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> uint(i%8*8))
		}
	}
	return hex.EncodeToString(b)
}

// slotArg reads a single 0-based slot index from a form field.
func slotArg(form url.Values, key string) (int, error) {
	n, err := strconv.Atoi(form.Get(key))
	if err != nil {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return n, nil
}

// slotsArg reads 2+ 0-based slot indices for a multi-swap. Accepts either
// repeated "slots" values (native HTML checkboxes) or a single
// space-separated value, whichever the client sent.
func slotsArg(form url.Values) ([]int, error) {
	raw := form["slots"]
	if len(raw) == 0 {
		return nil, fmt.Errorf("pick 2 or more slots")
	}
	var slots []int
	for _, v := range raw {
		for _, f := range strings.Fields(v) {
			n, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("invalid slots")
			}
			slots = append(slots, n)
		}
	}
	if len(slots) < 2 {
		return nil, fmt.Errorf("pick 2 or more slots")
	}
	return slots, nil
}

// targetArg decodes an opponent-card button's "player-slot" encoded target.
// ponytail: one combined field instead of separate player+slot form fields -
// every target is a server-generated button value, never hand-typed, so the
// combined encoding costs nothing and keeps board_frag.html's per-card button
// markup uniform.
func targetArg(form url.Values) (player, slot int, err error) {
	parts := strings.SplitN(form.Get("target"), "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid target")
	}
	if player, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, fmt.Errorf("invalid target")
	}
	if slot, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, fmt.Errorf("invalid target")
	}
	return player, slot, nil
}

// slotList formats 0-based slots as 1-based prose, e.g. "2 and 4".
func slotList(slots []int) string {
	parts := make([]string, len(slots))
	for i, s := range slots {
		parts[i] = strconv.Itoa(s + 1)
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
