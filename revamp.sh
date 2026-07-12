#!/usr/bin/env bash
# Run each Kapo UX Revamp task in a FRESH claude context (headless -p).
# Tasks run in order; each is a self-contained prompt with no shared session.
# ponytail: plain sequential loop — no orchestration lib, no parallelism.
#   Add xargs -P / a job queue only if wall-clock actually hurts.
set -euo pipefail
cd "$(dirname "$0")"

PLAN="Kapo UX Revamp Plan.md"
LOGDIR="revamp-logs"
mkdir -p "$LOGDIR"

# Shared preamble prepended to every task so the fresh context knows the repo.
PREAMBLE="You are working in the Kapo repo (Go + htmx + SSE card game).
Read '$PLAN' for full context before editing. Relevant files:
  pkg/server/view.go, pkg/server/templates/{base,board_frag,lobby,board,join,full}.html
The game engine pkg/game must NOT change. Build with 'go build ./...' when done.
Do only the task below, then stop.

TASK: "

# One entry per shippable unit of work, in dependency order (plan section 7).
tasks=(
  "p1-state-machine|Phase 1: In pkg/server/templates/base.html, collapse the three interaction paths (select-then-click, arm-then-click, drag & drop) into ONE source/target state machine. Implement armSource(kind) + resolveTarget(slot) shared by both click and drag handlers. Delete the standalone select mode. Multi-swap becomes a post-drop confirm; the server multiswap-* API is unchanged."
  "p1-prose-to-pill|Phase 1: Remove instruction <p> paragraphs from pkg/server/templates/board_frag.html. Add a You-pill that shows a single HintLine string plus the known total (with tooltip breakdown). Render 👁 badges from the existing 'seen' data. Add HintLine and PowerPreview (drawn-card rank -> power text) fields to BoardView in pkg/server/view.go and drop Flash prose replaced by toasts."
  "p2-reskin|Phase 2 reskin (no engine change): apply the visual system from the plan section 4 — Marcellus font for lettermark/nameplates/buttons/numerals, system-ui body, Georgia card faces; the green-felt + gold palette; 2.5:3.5 card faces with corner ranks + center pip and ✦ card backs; a single lift-pulse animation used ONLY for legal sources on your turn, static ring under prefers-reduced-motion. Edit templates + CSS only."
  "p3-lobby|Phase 3 lobby: merge the create form and waiting state in pkg/server/templates/lobby.html into one live seats screen — big table code, copy-invite button, 4 seat rows. Empty seats show a '+ bot' button that posts an add-bot action (no up-front bot count). Difficulty as a segmented EASY/HARD control with consequence text. DEAL disabled until >=2 players with a helper line. Ask name once on join."
  "p3-onboarding|Phase 3 onboarding: add the 3 in-game beats from plan section 3.3 (peek / first turn / powers), client-side only, gated by a per-hint localStorage flag. The '?' cheat-sheet chip is permanent. No server state."
  "p4-motion|Phase 4 motion pass: add the animations from plan section 5 (deal stagger, draw slide+flip, swap/take arcs, peek flip, kapo banner, round-over flip+count). All <=300ms except peeks; everything honors prefers-reduced-motion. Templates/CSS/JS only."
)

for entry in "${tasks[@]}"; do
  id="${entry%%|*}"
  prompt="${entry#*|}"
  log="$LOGDIR/$id.log"
  echo "=== $id ==="
  claude -p "$PREAMBLE$prompt" --permission-mode acceptEdits 2>&1 | tee "$log"
  echo "--- $id done -> $log ---"
done

echo "All tasks complete. Logs in $LOGDIR/"
