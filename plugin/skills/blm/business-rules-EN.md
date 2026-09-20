# Business rules — blm.md · proposals and conflict · report /blm · statistics

**[ไทย](business-rules.md)** · English

## What is blm.md

**One rules file, owner-decided.** `blm.md` holds the business logic as it is *now*, with `memory:` / `code:` / `verify:` links. Agents read it, report `/blm` self-tests (what they believed *before* reading vs. what the rules say), and propose changes as git-style conflict reports the owner resolves — the file never changes behind the owner's back. Stats show how often the rules pulled an agent back.

**One rules file `blm.md`** — what is true *now*, owned by the human. Each subtopic carries `memory:` `code:` `verify:` `updated_at:` refs, and every word that names a file becomes a link (`[db.ts](src/lib/db.ts)`, notes from the store or the mirror) — `linkPath` runs at checkout and on `blm conflict mark`.

**blm.md can be an index.** A row of its Main Business table may link a topic note — `| [Authentication](global/conventions/blm/blm-authentication.md) | one-line cue |` — and that note holds every rule block of the topic. `blm {query}` follows the links (refs point at the topic file), proposals and resolves land on the topic note, a missing link shows up as `(missing)`, and the core brief injected by the hook is exactly the table. Topic folder: `global/conventions/blm` on AgentsRoom, `blm` at the top of `<ai-dir>/memory` (`.claude/memory/blm/`) elsewhere — or `topicsFolder` in `.claude/blm.json` / env `BLM_TOPICS_FOLDER` (`BLM_MEMORY_DIR` moves the memory root).

- `blm {query?, trigger}` read rules (empty = whole file · word = matching blocks). Returns `refShort` as `<store>/blm.md:<line>` (clickable), `changedSinceLastRead`, `pendingDrafts`. · Owner says `/blm "<heading>"` at start of day. · Agent calls silently anytime understanding and code conflict.
- **Code contradicts rules = stop and report**. Don't edit code to match memory, don't edit rules yourself.
- Levels of rules: blm.md = main project rules. · notes in `features/<x>` = sub-rules of feature (line `memory:` points down). · sub-projects in folders (e.g., `wireguard/`) are major topics on their own, not noise.
- **Edit blm.md two ways**: rule meaning changed and owner hasn't decided yet → file a proposal (below). · Owner says fix a specific point clearly or it's a mechanical edit (link, spelling, filename) → `blm_patch {name:"blm", find, replace}`. Result stays local until you push.
- Owner changes rule = overrides old memory entirely → `blm_stat {event:"override", topic, note}` then edit the way above.

## Proposals and conflict — owner decides alone (`blm_conflict`)

**Conflicts and proposals the owner decides** — a clash between the local note and the backend, or an agent's proposal to change `blm.md`, becomes a git-style report `conflicts/[wait] <heading>-<time>.md` (Current = local / existing, Incoming = cloud / proposed). The owner ticks a box, or runs `blm conflicts -i` / `blm resolve <note> --keep incoming|current`; blm rewrites the note, renames the report to `[done]`, drops the `[Conflict](…)` marks and pushes. `blm conflict mark` places the marks; reports alone change nothing.

- `blm_conflict {topic, heading, reason, content}` = **proposal to change rules** (used at `/blm_init` or when rules should change). Current = block that exists (empty = new sub-heading). Incoming = content proposed. File again on same topic = replaces old version. · Topics split out as notes `blm-<topic>` already → report/resolve/push into that note (`blm resolve blm-<topic>`), not blm.md. Old proposals filed before the file split were redirected to the topic note themselves. · When resolving, the proposal is "appended" to the note's current content (no old snapshot that might be stale).
- `blm_conflict {name, topic, heading, reason}` = **real collision**. Draft in blm/ vs. cloud stacked (`blm_diff` shows). topic/heading = main/sub topic of blm.md that the note is part of.
- Both kinds get report `conflicts/[wait] <heading>-<time>.md` in git style: `***<<<<<<< Current …***` / `---` / `***>>>>>>> Incoming …***`. Owner edits the text on the side you'll keep. reason/content in owner's language. **Report only, don't touch any files**.
- `blm_conflict {action:"mark"}` (`blm conflict mark`). Place `[Conflict](path to report)` at line `memory:` before the link to the note that collided (or at the blm.md heading itself if it's a proposal · heading split out = before link in Main Business table row + at `## heading` in the topic note). Callable again, result the same. · Chains linkPath through blm.md too.
- Owner decides: check the box in the file and say "resolve". Or `blm conflicts` → `blm conflicts <id>` → `blm resolve <note> --keep incoming|current` (`-i` = interactive mode in real terminal). · Agent calls `blm_resolve {name, keep}` only when owner says so.
- Resolve succeeds = write result into note. Mark vanishes. Report renamed to `[done]`. Base moves forward. And **push that note to AgentsRoom right away** (close with `push:false`).
- `blm_conflicts` / `blm conflicts` see what's pending (`all:true` includes done). `blm status` has Conflicts section too. **Don't ask owner for a decision**. They'll decide when ready.

## Report /blm (self-test)

**Self-test report** — `/blm ["topic"]`: the agent writes what it believes *before* reading, then reports PASSED / NOT PASSED / UNKNOWN per subtopic with clickable `.agentsroom/blm/blm.md:<line>` refs. Columns are aligned by real monospace width (Thai combining vowels = 0, emoji = 2).

- NOT PASSED that you fix from reading the rules file → `blm_stat {event:"fixed", count}`.

## /blm_update — major topics new after /blm_init (owner 2026-09-20)

- `blm_update {topic?}` = `blm update` in terminal: report what blm.md covers without re-scanning, without LLM — topics that exist (note, block, oldest updated_at, folder that `code:` line cites) · clusters in graph with no rule citing them (candidates) · files changed since newest rule (`git log` only files in index; `no rule` = no rule yet / `covered — rule may be stale`) · specify topic = attach `blm_search` table of contents for that topic.
- Owner runs it first to pick topics, then says `/blm_update "<name>"` — agent reads hits → proposes meaning (cue ≤150 = note description + table cell) → sub-headings → confirm → `blm_create blm-<slug>` (folder = `topicsFolder` from `blm_status`) + `blm_patch` add row to table. · Unspecified = agent proposes 3–5 candidates from report to pick first.
- **Review page** (`blm update --html` / `blm_update {html:true}`): report as `<store>/reviews/review-<id>.json` + `.html` served at `http://127.0.0.1:<port>/r/<id>` (fixed port per project; in `blm mcp` server with process · CLI waits till submit) — data rows with checkable columns: `ref` (evidence) · `relate` (existing topics that relate — agent reads before proposing) · `target` (fold into this topic, single only → topic status `extend` + `existing`, write real with `blm_append`) · `ignore` (remember in `reviews/ignore.json`, blm update doesn't propose again) + field "tell agent" (`note`) → New topic / Add to <topic> → "blm topic create". · Next round, agent fills proposals into **same file** (`blm_update {from, proposal}`). Every point (main and sub topic name/meaning) has comment(+save) / agree / draft buttons. Every click writes to file right away. Main status changes and agent gets called (hook prompt injects `[blm] review submitted`) only on submit. · Text agent edits = original into `history` with comment. AGREE that didn't touch stays. · All AGREE = create note. · DRAFT pending = `blm update draft ["topic"]`. Gen HTML again from file: `blm update --html --from <json>`.
- Known limits: coverage counts as second-level folder (`src/lib` whole) — "changed since" part tells you the old rule might be stale, not the decider.

## /blm_init

- New project: explore (`blm_scan`, `blm_graph`, `blm_search`) → propose major topics → owner confirm → meaning → confirm → sub-headings per topic → blm.md.
- blm.md already exists = upgrade mode: don't overwrite. File block by block via `blm_conflict` (into topic note itself when topic split). New major topic = `blm_create {name:"blm-<slug>", folder:<topicFolder>, description:<cue same as table cell>}` then `blm_patch` add row `| [Topic](<topicFolder>/blm-<slug>.md) | cue |` to table. · Long draft split into sub-notes (`blm_create` e.g., `custom-vpn-rules`) then ref from the block.

## Statistics (like rtk gain — in `blm status`)

- `blm` self-logs read (`trigger:"user"` when owner says). · `blm_report` self-logs check. · `blm_stat {event:"lookup", query}` when owner asked and you looked up rules to answer. · `override` when owner changed rules. · `fixed` when rules came back correct.
