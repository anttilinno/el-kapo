# Next session plan

## Shipped today (2026-07-13)

Web UI (`pkg/server`) polish + hard-mode rules:

- **Cheat sheet** — added full point values (A/2–10/J/Q/K), the swap combo, and the
  `K K Q Q` auto-win.
- **Hard mode reveals** — only the 3 intentional peeks (starting pair, 7/8 own,
  9/10 opponent) reveal; swaps/takes/bot-moves stay face-down. Face-up penalty
  cards stay public. Deck shows thickness (full/half/low), no number.
- **Layout jumps fixed** — Kapo Call coin reserves its slot; power-preview text is
  off-flow with a reserved band; lobby seat rows constant height; helper line
  always reserved.
- **Lobby** — added remove-bot (`×`) button; add/remove no longer shifts the UI.
- **Opponent pacing** — bot turns paced to ~2s (`botTick`), and every move now
  animates a card onto the discard pile (`toss-in`, 450ms) so moves are
  followable instead of a hard-cut.

Tests: `TestHardMode*`, `TestEasyModeSwapReveals`, `TestRemoveBot`; `TestMain`
runs bots at 1ms so the suite stays ~0.6s. `go test ./...` green.

## Open choice — how far to take "follow the opponent's move"

Today's fix (2s pace + discard toss animation) makes it clear **that** a bot moved
and **what card** it discarded. It does NOT show the bot's *drawn* card or a
draw→place sequence. Pick one for next session:

- **Option A — Stop here (recommended).** Current state is followable and matches
  the ~2s/card industry norm. Zero further work. Choose this unless playtesting
  says the drawn card specifically needs surfacing.

- **Option B — Two-beat bot reveal.** Surface the current player's drawn card
  publicly in the DRAWN slot, broadcast, pause ~1s, then broadcast the placement.
  Requires: split `ai.Turn` (or `botLoop`) so the draw and the placement are two
  steps with a sleep between; expose the active player's drawn card to all viewers
  in `view.go`/`board_frag.html` (today the DRAWN pile only shows the viewer's own
  card). Medium effort, engine + view change.

Default if unsure: **A**.

## Not started / parked

- No play-by-play event log rendered (the `Log` field exists in the view, unused).
- Bot move still atomic server-side (relevant only if Option B is chosen).
