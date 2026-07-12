package server

import (
	"fmt"
	"strconv"

	"kapo/pkg/game"
)

// CardFace is the presentational decomposition of a card for the felt table:
// a rank string, a suit pip, and colouring flags. Hearts/diamonds render red;
// jokers render as a distinct star tile with no suit.
type CardFace struct {
	Rank  string // "A", "2".."10", "J", "Q", "K", or "JOKER"
	Pip   string // suit symbol, empty for jokers
	Red   bool   // hearts or diamonds
	Joker bool   // render the joker tile instead of rank+pip
}

// faceOf splits a game.Card into its display parts.
func faceOf(c game.Card) CardFace {
	if c.Rank == game.Joker {
		return CardFace{Rank: "JOKER", Joker: true}
	}
	var rank string
	switch c.Rank {
	case game.Ace:
		rank = "A"
	case game.Jack:
		rank = "J"
	case game.Queen:
		rank = "Q"
	case game.King:
		rank = "K"
	default:
		rank = strconv.Itoa(int(c.Rank))
	}
	pips := map[game.Suit]string{
		game.Clubs:    "♣",
		game.Diamonds: "♦",
		game.Hearts:   "♥",
		game.Spades:   "♠",
	}
	return CardFace{
		Rank: rank,
		Pip:  pips[c.Suit],
		Red:  c.Suit == game.Hearts || c.Suit == game.Diamonds,
	}
}

// CardView is one rendered card slot. Action is empty unless this exact card
// is a legal click target for the viewer right now; when set, the template
// renders a clickable hx-post button instead of a plain div.
type CardView struct {
	Idx    int    // 0-based slot index, sent as the "slot" form field
	Num    int    // 1-based, for display
	Label  string // card text, only meaningful if Show
	Show   bool   // viewer may see this card's rank/suit
	FaceUp bool   // publicly face-up (shown to every viewer)
	Action string // "", or "peek-setup"/"swap"/"take"/"peekown"/"arm-swap"/"peekother"/"swapother"
	Target string // "player-slot", set for opponent cards regardless of Action
	Armed  bool   // this own slot is the currently armed swap-other source

	// Presentation, populated alongside Label whenever Show is true.
	Rank  string
	Pip   string
	Red   bool
	Joker bool
}

// setFace fills both the legacy Label and the split rank/pip fields.
func (cv *CardView) setFace(c game.Card) {
	f := faceOf(c)
	cv.Label, cv.Rank, cv.Pip, cv.Red, cv.Joker = c.String(), f.Rank, f.Pip, f.Red, f.Joker
}

type PlayerView struct {
	Seat      int
	Name      string
	IsBot     bool
	IsMe      bool
	IsCurrent bool
	Pos       string // felt position: "me", "top", "left", "right"
	Cards     []CardView
	TableID   string // duplicated onto each seat so the "seat" partial can post moves
}

type ScoreView struct {
	Name  string
	Score int
}

type SeatView struct {
	Name    string
	Claimed bool
	IsBot   bool
	IsMe    bool
	Empty   bool // padding row: no player seated here yet
}

type TotalView struct {
	Name     string
	Total    int
	IsWinner bool
}

// BoardView is everything one viewer's render of board_frag.html needs. It
// is built fresh under the table lock for every render (SSE push or initial
// page load) - private knowledge differs per seat, so nothing is cached
// across viewers.
type BoardView struct {
	TableID string
	JoinURL string
	Phase   string // lobby, peeking, playing, roundover, matchover
	Me      int
	IsAdmin bool // viewer is the table creator (seat 0)
	Hard    bool // hard mode: reveals are timed, known-total helper hidden
	Round   int
	Flash   string
	Log     []string
	Scores  []ScoreView

	KapoActive bool
	KapoBy     string

	Players    []PlayerView
	HasDiscard bool
	DiscardTop CardFace
	DeckCount  int

	MyKnownTotal   int // points of the viewer's own cards they currently know
	MyUnknownCount int // viewer's own cards still hidden from them

	Seats   []SeatView // Lobby only, always padded to maxSeats rows
	CanDeal bool       // Lobby only: >=2 players seated

	MyPeeksLeft int // Peeking only, humans only

	MyTurn        bool     // Playing only
	MyPhase       string   // turnstart, drawn, action
	Drawn         CardFace // the card the viewer just drew (MyPhase == "drawn")
	PendingAction string   // peekown, peekother, swapother
	HintLine      string   // one-line contextual guidance shown in the You pill
	PowerPreview  string   // discard power the current drawn card would trigger (MyPhase == "drawn")

	Totals      []TotalView // RoundOver
	Winners     []string
	AutoWin     string
	CanNext     bool
	MatchWinner string // MatchOver
}

func phaseName(p TablePhase) string {
	switch p {
	case Lobby:
		return "lobby"
	case Peeking:
		return "peeking"
	case Playing:
		return "playing"
	case RoundOver:
		return "roundover"
	case MatchOver:
		return "matchover"
	case Ended:
		return "ended"
	default:
		return ""
	}
}

func actionName(a game.ActionType) string {
	switch a {
	case game.ActionPeekOwn:
		return "peekown"
	case game.ActionPeekOther:
		return "peekother"
	case game.ActionSwapOther:
		return "swapother"
	default:
		return ""
	}
}

// powerText describes the discard power a drawn card of this rank triggers,
// previewed on the discard target before the player commits. Empty for ranks
// with no power. Mirrors game.actionFor's rank->action mapping.
func powerText(r game.Rank) string {
	switch r {
	case 7, 8:
		return "discard to peek one of your own cards"
	case 9, 10:
		return "discard to peek an opponent's card"
	case game.Jack, game.Queen:
		return "discard to blind-swap with an opponent"
	default:
		return ""
	}
}

func seatLabel(s seat, i int) string {
	if s.name != "" {
		return s.name
	}
	return fmt.Sprintf("seat %d (empty)", i+1)
}

// winnersDisplay collapses lastResult down to the actual winning seat(s),
// honoring an auto-win override same as cmd/kapo/main.go's playRound does.
func (t *Table) winnersDisplay() []int {
	if t.lastResult.AutoWin >= 0 {
		return []int{t.lastResult.AutoWin}
	}
	return t.lastResult.Winners
}

// buildView assembles the BoardView for viewer. Caller must hold t.mu.
func (t *Table) buildView(viewer int) BoardView {
	v := BoardView{
		TableID: t.id,
		Phase:   phaseName(t.phase),
		Me:      viewer,
		IsAdmin: viewer == 0,
		Hard:    t.hard,
		Round:   t.round,
		Flash:   t.flash[viewer],
		Log:     t.log,
	}
	if t.phase == Lobby {
		v.JoinURL = "/games/" + t.id
		v.CanDeal = len(t.seats) >= 2
		for i := 0; i < maxSeats; i++ {
			if i < len(t.seats) {
				s := t.seats[i]
				v.Seats = append(v.Seats, SeatView{Name: s.name, Claimed: true, IsBot: s.isBot, IsMe: i == viewer})
			} else {
				v.Seats = append(v.Seats, SeatView{Empty: true})
			}
		}
		return v
	}

	// match exists from Deal onward; Scores drive the header chips. Guarded
	// because the admin can End the game straight from the lobby (match nil).
	for i, s := range t.seats {
		score := 0
		if t.match != nil {
			score = t.match.Scores[i]
		}
		v.Scores = append(v.Scores, ScoreView{Name: seatLabel(s, i), Score: score})
	}
	if t.g == nil {
		return v
	}

	if kc := t.g.KapoCaller(); kc >= 0 {
		v.KapoActive = true
		v.KapoBy = t.seats[kc].name
	}
	if top, ok := t.g.TopDiscard(); ok {
		v.HasDiscard = true
		v.DiscardTop = faceOf(top)
	}
	v.DeckCount = t.g.DeckLen()

	// Viewer's own running total, over only the cards they currently know.
	for slot, c := range t.g.Hand(viewer) {
		if _, up := t.g.FaceUpCard(viewer, slot); up {
			v.MyKnownTotal += c.Points()
		} else if kc, ok := t.g.KnownTo(viewer, viewer, slot); ok {
			v.MyKnownTotal += kc.Points()
		} else {
			v.MyUnknownCount++
		}
	}

	// Rectangular seating rotated so the viewer sits at the bottom; the
	// following seats in turn order wrap left -> top -> right around the felt.
	n := len(t.seats)
	sides := map[int][]string{2: {"top"}, 3: {"left", "right"}, 4: {"left", "top", "right"}}[n]
	pos := make([]string, n)
	pos[viewer] = "me"
	for i := 1; i < n; i++ {
		pos[(viewer+i)%n] = sides[i-1]
	}

	for i, s := range t.seats {
		pv := PlayerView{
			Seat:      i,
			Name:      seatLabel(s, i),
			IsBot:     s.isBot,
			IsMe:      i == viewer,
			IsCurrent: t.phase == Playing && i == t.g.Current(),
			Pos:       pos[i],
			TableID:   t.id,
		}
		for slot := range t.g.Hand(i) {
			pv.Cards = append(pv.Cards, t.cardView(viewer, i, slot))
		}
		v.Players = append(v.Players, pv)
	}
	t.annotateActions(viewer, v.Players)

	switch t.phase {
	case Peeking:
		if !t.seats[viewer].isBot {
			v.MyPeeksLeft = 2 - len(t.seats[viewer].peekSlots)
			if v.MyPeeksLeft > 0 {
				v.HintLine = "Tap a card to memorize it"
			} else {
				v.HintLine = "Waiting for other players to finish peeking…"
			}
		}
	case Playing:
		v.MyTurn = viewer == t.g.Current()
		if v.MyTurn {
			switch t.g.Phase() {
			case game.TurnStart:
				v.MyPhase = "turnstart"
				if v.HasDiscard {
					v.HintLine = "Your turn — tap the deck to draw, or take the discard"
				} else {
					v.HintLine = "Your turn — tap the deck to draw"
				}
			case game.Drawn:
				v.MyPhase = "drawn"
				v.Drawn = faceOf(t.drawnCard)
				v.PowerPreview = powerText(t.drawnCard.Rank)
				v.HintLine = "Place it on one of your cards, or discard it"
			case game.Action:
				v.MyPhase = "action"
				v.PendingAction = actionName(t.pendingAction)
				switch t.pendingAction {
				case game.ActionPeekOwn:
					v.HintLine = "Pick one of your cards to peek at it"
				case game.ActionPeekOther:
					v.HintLine = "Pick an opponent's card to peek at it"
				case game.ActionSwapOther:
					v.HintLine = "Pick your card, then an opponent's to swap"
				}
			}
		} else {
			cur := t.g.Current()
			v.HintLine = seatLabel(t.seats[cur], cur) + " is thinking…"
		}
	case RoundOver:
		winSet := map[int]bool{}
		for _, w := range t.winnersDisplay() {
			winSet[w] = true
		}
		for i, tot := range t.lastResult.Totals {
			v.Totals = append(v.Totals, TotalView{Name: t.seats[i].name, Total: tot, IsWinner: winSet[i]})
		}
		for _, w := range t.winnersDisplay() {
			v.Winners = append(v.Winners, t.seats[w].name)
		}
		if t.lastResult.AutoWin >= 0 {
			v.AutoWin = t.seats[t.lastResult.AutoWin].name
		}
		v.CanNext = !t.seats[viewer].isBot
	case MatchOver:
		v.MatchWinner = t.seats[t.match.Winner()].name
	}
	return v
}

// cardView resolves what viewer may see of player's slot: face-up cards are
// public to everyone, everything else only if the viewer has personally
// seen it (own peeks, granted peeks, or a public move that revealed it). At
// RoundOver/MatchOver the engine's own state is fully public, so show all.
// Hard mode adds a time gate on top of that: engine-visible cards render
// face-down again once their reveal window expires.
func (t *Table) cardView(viewer, player, slot int) CardView {
	cv := CardView{Idx: slot, Num: slot + 1, Target: fmt.Sprintf("%d-%d", player, slot)}

	if t.phase == RoundOver || t.phase == MatchOver {
		cv.setFace(t.g.Hand(player)[slot])
		cv.Show = true
		_, cv.FaceUp = t.g.FaceUpCard(player, slot)
		return cv
	}
	if c, ok := t.g.FaceUpCard(player, slot); ok {
		if t.revealActive(viewer, player, slot) {
			cv.setFace(c)
			cv.Show = true
			cv.FaceUp = true
		}
		return cv
	}
	if c, ok := t.g.KnownTo(viewer, player, slot); ok && t.revealActive(viewer, player, slot) {
		cv.setFace(c)
		cv.Show = true
	}
	return cv
}

// annotateActions marks which cards are legal click targets for viewer right
// now, mutating the Cards already attached to players in place.
func (t *Table) annotateActions(viewer int, players []PlayerView) {
	switch t.phase {
	case Peeking:
		if t.seats[viewer].isBot || 2-len(t.seats[viewer].peekSlots) <= 0 {
			return
		}
		alreadyPeeked := func(slot int) bool {
			for _, s := range t.seats[viewer].peekSlots {
				if s == slot {
					return true
				}
			}
			return false
		}
		for pi := range players {
			if !players[pi].IsMe {
				continue
			}
			for ci := range players[pi].Cards {
				if !alreadyPeeked(players[pi].Cards[ci].Idx) {
					players[pi].Cards[ci].Action = "peek-setup"
				}
			}
		}

	case Playing:
		if viewer != t.g.Current() {
			return
		}
		switch t.g.Phase() {
		case game.TurnStart:
			if _, ok := t.g.TopDiscard(); !ok {
				return
			}
			for pi := range players {
				if players[pi].IsMe {
					for ci := range players[pi].Cards {
						players[pi].Cards[ci].Action = "take"
					}
				}
			}
		case game.Drawn:
			for pi := range players {
				if players[pi].IsMe {
					for ci := range players[pi].Cards {
						players[pi].Cards[ci].Action = "swap"
					}
				}
			}
		case game.Action:
			switch t.pendingAction {
			case game.ActionPeekOwn:
				for pi := range players {
					if players[pi].IsMe {
						for ci := range players[pi].Cards {
							players[pi].Cards[ci].Action = "peekown"
						}
					}
				}
			case game.ActionPeekOther:
				for pi := range players {
					if !players[pi].IsMe {
						for ci := range players[pi].Cards {
							players[pi].Cards[ci].Action = "peekother"
						}
					}
				}
			case game.ActionSwapOther:
				for pi := range players {
					if players[pi].IsMe {
						for ci := range players[pi].Cards {
							players[pi].Cards[ci].Action = "arm-swap"
							players[pi].Cards[ci].Armed = players[pi].Cards[ci].Idx == t.armedSlot
						}
					} else {
						for ci := range players[pi].Cards {
							players[pi].Cards[ci].Action = "swapother"
						}
					}
				}
			}
		}
	}
}
