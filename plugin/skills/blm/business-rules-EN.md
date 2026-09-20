# Business rules — blm.md · proposals and conflict · report /blm · statistics

**[ไทย](business-rules.md)** · English

## What is blm.md

- "Rules that are true right now" for the project. Owner is the decider. · File begins with table `## Main Business`. · Each sub-heading is a block with `memory:` `code:` `verify:` `updated_at/by`. · Every word that names a file is a link `[name](path from repo root)` (linkPath does it at checkout and during `blm conflict mark`).
- `blm {query?, trigger}` read rules (empty = whole file · word = matching blocks). Returns `refShort` as `<store>/blm.md:<line>` (clickable), `changedSinceLastRead`, `pendingDrafts`. · Owner says `/blm "<heading>"` at start of day. · Agent calls silently anytime understanding and code conflict.
- **Code contradicts rules = stop and report**. Don't edit code to match memory, don't edit rules yourself.
- **blm.md can be a table of contents** (owner 2026-09-20): rows in Main Business table are links `[Topic](<topicFolder>/blm-<topic>.md)` → all rules for that topic live in note `blm-<topic>` (front matter `folder:` = topicFolder), and `blm {query}` follows the link (ref points to topic file). Topics not yet split out stay under `# Topic` in blm.md the same way. Both styles can coexist. · Missing links show as `(missing)` block and in `blm status`. · topicFolder: agentsroom = `global/conventions/blm` · other backends = `blm` at top of `<ai-dir>/memory` (`.claude/memory/blm/`) · set yourself in `.claude/blm.json` `topicsFolder` or env `BLM_TOPICS_FOLDER` (`BLM_MEMORY_DIR` moves memory root). · core brief that hook injects = this table exactly.
- Levels of rules: blm.md = main project rules. · notes in `features/<x>` = sub-rules of feature (line `memory:` points down). · sub-projects in folders (e.g., `wireguard/`) are major topics on their own, not noise.
- **Edit blm.md two ways**: rule meaning changed and owner hasn't decided yet → file a proposal (below). · Owner says fix a specific point clearly or it's a mechanical edit (link, spelling, filename) → `blm_patch {name:"blm", find, replace}`. Result stays local until you push.
- Owner changes rule = overrides old memory entirely → `blm_stat {event:"override", topic, note}` then edit the way above.

## Proposals and conflict — owner decides alone (`blm_conflict`)

- `blm_conflict {topic, heading, reason, content}` = **proposal to change rules** (used at `/blm_init` or when rules should change). Current = block that exists (empty = new sub-heading). Incoming = content proposed. File again on same topic = replaces old version. · Topics split out as notes `blm-<topic>` already → report/resolve/push into that note (`blm resolve blm-<topic>`), not blm.md. Old proposals filed before the file split were redirected to the topic note themselves. · When resolving, the proposal is "appended" to the note's current content (no old snapshot that might be stale).
- `blm_conflict {name, topic, heading, reason}` = **real collision**. Draft in blm/ vs. cloud stacked (`blm_diff` shows). topic/heading = main/sub topic of blm.md that the note is part of.
- Both kinds get report `conflicts/[wait] <heading>-<time>.md` in git style: `***<<<<<<< Current …***` / `---` / `***>>>>>>> Incoming …***`. Owner edits the text on the side you'll keep. reason/content in owner's language. **Report only, don't touch any files**.
- `blm_conflict {action:"mark"}` (`blm conflict mark`). Place `[Conflict](path to report)` at line `memory:` before the link to the note that collided (or at the blm.md heading itself if it's a proposal · heading split out = before link in Main Business table row + at `## heading` in the topic note). Callable again, result the same. · Chains linkPath through blm.md too.
- Owner decides: check the box in the file and say "resolve". Or `blm conflicts` → `blm conflicts <id>` → `blm resolve <note> --keep incoming|current` (`-i` = interactive mode in real terminal). · Agent calls `blm_resolve {name, keep}` only when owner says so.
- Resolve succeeds = write result into note. Mark vanishes. Report renamed to `[done]`. Base moves forward. And **push that note to AgentsRoom right away** (close with `push:false`).
- `blm_conflicts` / `blm conflicts` see what's pending (`all:true` includes done). `blm status` has Conflicts section too. **Don't ask owner for a decision**. They'll decide when ready.

## Report /blm (self-test)

- `/blm ["topic"]`: agent writes what they believe **before** reading rules (Before) → `blm {query}` → compare heading by heading PASSED / NOT PASSED / UNKNOWN with ref `<store>/blm.md:<line>` → After. · `blm_report {rows, before, after}` arranges columns by real display width (Thai vowels 0 width, emoji 2).
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
