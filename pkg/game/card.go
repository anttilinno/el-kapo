package game

import (
	"fmt"
	"math/rand"
)

type Suit int

const (
	Clubs Suit = iota
	Diamonds
	Hearts
	Spades
	NoSuit // jokers
)

var suitSymbols = [...]string{"♣", "♦", "♥", "♠", ""}

type Rank int

const (
	Joker Rank = 0
	Ace   Rank = 1
	Jack  Rank = 11
	Queen Rank = 12
	King  Rank = 13
)

type Card struct {
	Rank Rank
	Suit Suit
}

// Points: ace 1, numbers face value, jack 11, queen 12, king 13, joker 0.
func (c Card) Points() int { return int(c.Rank) }

func (c Card) String() string {
	switch c.Rank {
	case Joker:
		return "Joker"
	case Ace:
		return "A" + suitSymbols[c.Suit]
	case Jack:
		return "J" + suitSymbols[c.Suit]
	case Queen:
		return "Q" + suitSymbols[c.Suit]
	case King:
		return "K" + suitSymbols[c.Suit]
	default:
		return fmt.Sprintf("%d%s", c.Rank, suitSymbols[c.Suit])
	}
}

// NewDeck: standard deck without kings, plus 2 kings and 2 jokers = 52 cards.
func NewDeck(rng *rand.Rand) []Card {
	deck := make([]Card, 0, 52)
	for s := Clubs; s <= Spades; s++ {
		for r := Ace; r <= Queen; r++ {
			deck = append(deck, Card{r, s})
		}
	}
	deck = append(deck,
		Card{King, Spades}, Card{King, Hearts},
		Card{Joker, NoSuit}, Card{Joker, NoSuit},
	)
	rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	return deck
}
