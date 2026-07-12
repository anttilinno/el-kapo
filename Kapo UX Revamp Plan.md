# Kapo UX Revamp — Implementation Plan

**Date:** July 2026 · **Target:** desktop/web, real-time multiplayer, 2–4 players, mixed audience
**Design artifacts:** `Kapo Wireframes.dc.html` (exploration), `Kapo Table.dc.html` (hi-fi static), `Kapo Table v2.dc.html` (hi-fi interactive), `Kapo Lobby.dc.html`, `Kapo Onboarding.dc.html`

---

## 1. Problem statement

The current UI (Go + htmx + SSE) works, but:

1. **Three competing interaction systems** for the same move: select-then-click, arm-then-click, and drag & drop. Players must discover all three and the modes can conflict.
2. **Instruction paragraphs instead of affordances.** Turn guidance is prose under the felt ("Swap: click or drag the drawn card onto your card…"), which nobody reads twice and novices misread once.
3. **No onboarding.** The setup peek, discard powers (7–Q), and Kapo itself are only explained by playing wrong.
4. **Lobby friction.** Create form → separate waiting page → join form; bots configured up front instead of as seats fill.
5. **Style debt.** The casino theme is right in spirit but flat in execution (system fonts, harsh gradients, glow noise).

## 2. Chosen direction: the "One Verb" table (wireframe 1a)

Every move is **move a card from a source to a target**. Drag is the primary gesture; *tap source → tap target* is the exact click equivalent (same mental model, works for accessibility and trackpads). Nothing else on the table is interactive.

| Move | Source → Target |
|---|---|
| Draw | Deck → (drawn slot appears) |
| Draw & swap in one gesture | Deck → your slot |
| Keep drawn card | Drawn card → your slot |
| Discard drawn card | Drawn card → discard pile |
| Take discard | Discard top → your slot |
| Multi-swap | Drop on one slot, matching known slots wiggle — tap to add, then confirm |

**Removed:** select mode, arm mode as separate systems; all instruction paragraphs; the always-visible debug log.

### Affordance rules
- On your turn, **legal sources pulse gently** (gold ring, 1.8s ease). Nothing pulses off-turn.
- **Drop targets show a dashed ring only while a source is armed/dragged.**
- **👁 badge** on your cards you've seen (easy mode) replaces the "known total" paragraph; the running total lives in the **You pill** (`known: 10 pts (+2 hidden)`).
- The You pill carries a single contextual hint (`your turn — drag a card` / `place it or discard it`), replacing all prose.
- Discard **powers are previewed before commit** (toast/label on the discard target: "9 → peek an opponent's card") — kills the "what did discarding just do?" confusion.

## 3. Screens

### 3.1 Play screen (`Kapo Table v2.dc.html`)
- **Layout:** rectangular felt; viewer bottom, opponents wrap left → top → right in turn order (2P: top; 3P: left+right; 4P: all three). Empty sides collapse.
- **Header:** KAPO lettermark, round number, Kapo banner (when called), score chips per player.
- **Center strip:** deck (with count), drawn card (only during decision, lifted + gold glow), discard top.
- **Own hand:** larger cards (~1.2× opponents), 👁 badges, You pill beneath.
- **CALL KAPO:** dedicated red/gold button, bottom-right of felt; disabled (dimmed) unless at turn start and not already called.
- **"?" chip** bottom-left: hover/click cheat sheet (powers, K/Joker values, Kapo rule).
- **Turn feedback:** current player's nameplate glows; off-turn the You pill reads "Mara is thinking…".

### 3.2 Lobby (`Kapo Lobby.dc.html`)
- Create + wait merged into **one live screen**: big table code, copy-invite button, 4 seat rows.
- Empty seats show "waiting for the link…" + inline **+ bot** button (bots added as needed, not via a count field up front).
- Difficulty as a segmented control (EASY: seen cards stay visible / HARD: memory only) with the consequence written on the control.
- **DEAL FIRST ROUND** disabled until ≥2 players; helper line explains why.
- Name is asked once on join (not per-form).

### 3.3 Onboarding (`Kapo Onboarding.dc.html`) — 3 beats, all in-game, no rules wall
1. **The peek** — coach chip "Tap 2 of your cards to memorize them"; peeked card shows its **point value on the face**; 5s ring timer; rest of table dimmed to 35%.
2. **First turn** — piles pulse; chip: "Lowest hand total wins. Take a card from either pile." One hint per new verb, first time only (stored per browser).
3. **Powers** — the "?" chip pulses once; cheat sheet panel: 7·8 peek yours, 9·10 peek theirs, J·Q blind swap, K♠♥=13, Joker=0, Kapo rule.

Beat hints appear only in a player's first match; the "?" chip is permanent.

## 4. Visual system

- **Type:** Marcellus (Google Fonts) for lettermark, nameplates, buttons, numerals; system-ui for body; Georgia for card faces.
- **Palette:** near-black warm surround `#0a0c0a→#050605`; felt radial `#1d5c3a → #092417`; gold `#d3af5e` / bright `#f2d894` / dim `#8a7327`; card ivory `#fdfbf3→#efe8d4`; red suits `#b3402a`; card backs burgundy stripe `#6d1b20/#54121a` with gold inner frame; joker accent violet `#5a2d82`.
- **Cards:** 2.5:3.5 ratio, rank in both corners + large center pip; backs: ✦ ornament in a gold frame.
- **Glow discipline:** one pulse animation (`lift-pulse`), used only for legal sources on your turn; reduced-motion → static ring.

## 5. Motion (moderate)

| Moment | Animation |
|---|---|
| Deal | Cards fly from deck to each seat, 40ms stagger |
| Draw | Deck top slides to drawn slot, flips face-up (200ms) |
| Swap/take | Card arcs to slot; displaced card arcs to discard |
| Peek | Card flips up 1.5s, flips back; HARD mode uses the timed ring from onboarding |
| Kapo call | Banner drops in header; felt rail flashes once |
| Round over | All hands flip up in sequence, totals count up |

All ≤300ms except peeks; everything honors `prefers-reduced-motion`.

## 6. Mapping to the existing codebase

The engine (`pkg/game`) needs **no changes**. Server/templates:

- **`base.html` JS:** collapse the three interaction paths into one `source/target` state machine: `armSource(kind)` + `resolveTarget(slot)` used by both click and drag handlers. Delete the standalone select mode; multi-swap becomes a post-drop confirm (server API `multiswap-*` unchanged).
- **`board_frag.html`:** remove instruction `<p>`s; add You-pill hint string from `BoardView` (one short field, e.g. `HintLine`); render 👁 badges from existing `seen` data; move known total into the pill (tooltip for breakdown).
- **`view.go`:** add `HintLine` and `PowerPreview` (rank → power text for the current drawn card) to `BoardView`; drop `Flash` prose where replaced by toasts.
- **Lobby:** merge `lobby.html` create form + waiting state into the live seats screen; `+ bot` posts an add-bot action instead of an up-front count.
- **Onboarding:** client-side only — localStorage flag per hint; no server state.
- **Debug log:** unchanged behind the admin toggle, but never rendered for non-admins.

## 7. Rollout

1. **Phase 1 — interaction unification** (highest value, lowest risk): one-verb state machine, remove prose, You-pill hints, 👁 badges. No visual redesign needed to ship this.
2. **Phase 2 — reskin:** fonts, palette, card faces, pulse discipline.
3. **Phase 3 — lobby merge + onboarding beats.**
4. **Phase 4 — motion pass** (deal/draw/swap/flip animations).

Each phase is independently shippable; 1 and 2 don't touch the Go engine at all.

## 8. Open questions

- Multi-swap confirm UX: the "wiggle + tap to add" pattern needs a playtest vs. a small confirm pill.
- Power actions (peek/swap targets) still use direct card-click on highlighted targets — validate that this reads as the same verb ("move your attention to a card").
- HARD mode reveal timer length (currently 5s in onboarding) — tune with players.
- Spectator/disconnected-player view was out of scope.
