# blm — business-logic-memory

One binary. Session temp memory for AI agents + a single business-logic source of truth (`blm.md`) they test themselves against + semantic code search that tells you how far to trust each hit.

## The problem

Working with coding agents on a real product, every session, the same things went wrong:

- **Memory was slow and dangerous.** The shared project memory (AgentsRoom) is the right place for decisions, but writing to it mid-task took 3–10 s (sometimes over two minutes) and an agent that wrote without reading first overwrote a note wholesale. So agents either stopped writing — and the session compacted with the findings still only in chat — or wrote and broke something.
- **Business logic lived nowhere in particular.** It was spread across dozens of notes, code comments and the owner's head. Agents "remembered" a rule, built on it, and were wrong; the owner found out from the result. There was no single file that says *what is true now*, no way for an agent to test its own understanding against it, and no place for the owner to decide when the agent proposes a change.
- **Code search was either blind or bloated.** `grep` finds strings you already know; a semantic index (SocratiCode) finds meaning but returns ten full code chunks per question, needs a local LLM + vector database eating ~1 GB of RAM on the dev machine, and cannot tell you whether a hit matched because of the code or because of a comment that may no longer be true.
- **Agents drift off the concept.** Every new session — and every auto-compaction — starts an agent that no longer holds the project's goals and core rules, so it "helps" with things nobody asked for or builds on a rule it half-remembers. Re-briefing it by hand costs 30 minutes to an hour, every time, and still fades mid-task.
- **Comments and docs drift.** A comment, a spec, a migration file, even the project's own CLAUDE.md can describe a state the code left behind; a search that trusts them equally sends the agent down the wrong path with full confidence.

## What blm does

- **Local first, sync once.** Notes are edited on disk in milliseconds (`blm_create` / `blm_append` / `blm_patch`, checked out from the backend with a base snapshot, history for every change) and pushed to AgentsRoom in one step when the owner says "update memory". No note content passes through the agent's context; a clash with a newer cloud version is reported, never overwritten. Works with AgentsRoom (recommended), Obsidian, a custom CLI, or no backend at all.
- **One rules file, owner-decided.** `blm.md` holds the business logic as it is *now*, with `memory:` / `code:` / `verify:` links. Agents read it, report `/blm` self-tests (what they believed *before* reading vs. what the rules say), and propose changes as git-style conflict reports the owner resolves — the file never changes behind the owner's back. Stats show how often the rules pulled an agent back.
- **The concept comes back by itself.** One central hook (`blm hook <event>`) injects the *core* of `blm.md` — the Main Business table, the list of rule blocks and three standing rules — at every session start, including right after auto-compaction, and again before a prompt or after edits **only when a debounce has expired** (20 min by default, or when `blm.md` changed), so an agent editing fifty files in a row is not re-briefed fifty times. `/blm` lets the owner check at any moment whether the agent and the rules still agree.
- **Code search that grades its own hits.** `blm search` uses the SocratiCode index (same hybrid dense + BM25 query, same scores) but answers with a two-line table of contents per hit instead of ten code chunks, opens only the chunks you pick with real file lines, drops notes/memory `.md` from results, and adds an **origin** column: whether the query matched the *code* or only a *comment*, and whether the comment agrees with its code (`code` · `both ✓` · `comment ⚠`). Thai or English questions, no symbol names needed.
- **Current state, not history.** For SQL, only a generated per-table schema snapshot (`db/schema/<table>.sql`, refreshed after every dev migration) is indexed — migrations and dumps are not — so "which table stores X" lands on one true answer.
- **The stack where it belongs.** Ollama + Qdrant run on a GPU box (`blm tools install socraticode --remote <host>`), the dev machine keeps nothing resident, and the same command wires Claude Code hooks (index state at session start, top-3 pointers per prompt, a nudge when an agent greps instead of asking) and a statusline (`socraticode : online · 4213 nodes / 24326 edges · ✓ synced`).
- **One plugin, self-updating.** `blm init` installs the binary, the Claude Code plugin and the hooks; `blm self-update` refreshes both. CLI and MCP share one code path, so what the owner sees in the terminal is what the agent sees.

Where it goes next: hosting SocratiCode's indexer as a child process so blm is the only plugin, tuning the origin thresholds against measured cases, a one-command server install for the GPU stack, and a `--check` in CI for the schema snapshot.

- **The store is the working copy** — `.agentsroom/blm/` holds `blm.md` and every note the agent touched, checked out from the backend with a base snapshot; the backend mirror is only there to tell who is newer. Notes are `synced` or `edited`; a push leaves them in place and advances the base.
- **Jot fast during the session** — `blm_create` (new note only) · `blm_append` · `blm_patch` (find/replace, checks the note out first) · `blm_replace` (whole note, asks for `confirm:true` when the new content differs a lot). Local files, milliseconds; the slow backend (AgentsRoom, a wiki, a CLI) is synced once when the owner says so.
- **One rules file `blm.md`** — what is true *now*, owned by the human. Each subtopic carries `memory:` `code:` `verify:` `updated_at:` refs, and every word that names a file becomes a link (`[db.ts](src/lib/db.ts)`, notes from the store or the mirror) — `linkPath` runs at checkout and on `blm conflict mark`.
- **`blm.md` can be an index.** A row of its Main Business table may link a topic note — `| [Authentication](global/conventions/blm/blm-authentication.md) | one-line cue |` — and that note holds every rule block of the topic. `blm {query}` follows the links (refs point at the topic file), proposals and resolves land on the topic note, a missing link shows up as `(missing)`, and the core brief injected by the hook is exactly the table. Topic folder: `global/conventions/blm` on AgentsRoom, `blm` at the top of `<ai-dir>/memory` (`.claude/memory/blm/`) elsewhere — or `topicsFolder` in `.claude/blm.json` / env `BLM_TOPICS_FOLDER` (`BLM_MEMORY_DIR` moves the memory root).
- **Conflicts and proposals the owner decides** — a clash between the local note and the backend, or an agent's proposal to change `blm.md`, becomes a git-style report `conflicts/[wait] <heading>-<time>.md` (Current = local / existing, Incoming = cloud / proposed). The owner ticks a box, or runs `blm conflicts -i` / `blm resolve <note> --keep incoming|current`; blm rewrites the note, renames the report to `[done]`, drops the `[Conflict](…)` marks and pushes. `blm conflict mark` places the marks; reports alone change nothing.
- **Self-test report** — `/blm ["topic"]`: the agent writes what it believes *before* reading, then reports PASSED / NOT PASSED / UNKNOWN per subtopic with clickable `.agentsroom/blm/blm.md:<line>` refs. Columns are aligned by real monospace width (Thai combining vowels = 0, emoji = 2).
- **Stats like `rtk gain`** — how often the agent was wrong, how often the rules file pulled it back, how often the human had to change a rule.
- **History** — every change/delete snapshots the old file to `history/<name>-[action]-YYYYMMDD-HHmmss.md`; `blm restore <history-file> <note>` puts one back (`blm restore <note>` lists them).
- **Write lock** — Edit/Write denied, `blm guard` PreToolUse hook, optional OS sandbox `denyWrite`. Only `blm` writes the store.
- **Cross-platform** — Go binary for macOS / Linux / Windows. Users call `blm status` directly; agents call the same code through MCP (`blm mcp`).

## Install (one line, inside your project)

> Run the install command **from the root of the project** blm should remember (the folder with `.git` / `.agentsroom` / `package.json` …). `blm init` refuses to run elsewhere unless you pass `--force`. Without a backend flag it auto-detects: existing `.claude/blm.json` → kept · `.agentsroom/` → agentsroom · `.obsidian/` → obsidian · otherwise none.

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.sh | sh -s -- --agentsroom
# Windows (PowerShell)
$env:BLM_INIT_ARGS="--agentsroom"; irm https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.ps1 | iex
# Go toolchain
go install github.com/Ekkapap/business-logic-memory/cmd/blm@latest && blm path && blm init --agentsroom
# Homebrew (tap coming)
```

The installer puts the binary at `~/.blm/bin/blm` (Windows: `~\.blm\bin\blm.exe`) and links `~/.local/bin/blm` to it; `blm self-update` replaces that file from the latest GitHub release. Developers: `make install` builds into the same `~/.blm/bin` (the repo stays a clean checkout for commits), `make install-dev` links `~/.local/bin/blm` to the repo's `./bin/blm` instead, in which case `blm self-update` does `git pull` + `go build` there.

`blm init` options — the backend decides where the truth lives:

| flag | store | sync |
| --- | --- | --- |
| *(none)* | `.claude/blm` | none — files are the truth (`blm_sync` not exposed) |
| `--agentsroom` | `.agentsroom/blm` + mirror `.agentsroom/memory` | push = `memory_save` plan, pull = refresh mirror |
| `--obsidian` | `blm/` (visible in the vault) | none |
| `--dir <p> --backend <cli>` | `<p>` | agent learns push/pull from `<cli> --help` during `/blm_init` |

Also: `--tools socraticode,obsidian,graphify` (neighbour tools to track; default = whatever is installed), `--sandbox` (enable Claude Code OS sandbox in user settings), `--no-plugin`, `--marketplace <dir|owner/repo>`.
`init` writes `.claude/blm.json`, the store, `.ignorememory`, merges `.claude/settings.json` (deny rules, sandbox denyWrite, `blm guard` hook), adds `blm` to PATH (or prints the command if it can't), and registers the Claude Code plugin. Re-running is idempotent. Old `.agentsroom/memory-temp` stores are migrated.

Then in Claude Code: `/reload-plugins` → `/blm_init` (guided analysis: main topics → confirm → meanings → confirm → subtopics per topic → `blm.md`).

## Commands

| user (terminal, no AI) | agent (MCP tool) | slash |
| --- | --- | --- |
| `blm status [--json]` — readiness first (`READY` / `NOT READY <why>`), then paths, rules, temp notes, tools, stats; rtk-gain style, colours on a TTY | `blm_status` | `/blm_status` |
| `blm report [name]` | `blm_report` | `/blm_report` |
| `blm tools <action> [tool]` | `blm_tools` | `/blm_tools` |
| `blm sync --push \| --pull [--apply]` | `blm_sync` | — |
| `blm create/append/patch/replace <name> …` · `blm get/delete <name>` | `blm_create` · `blm_append` · `blm_patch` · `blm_replace` · `blm_get` · `blm_delete` | `create` refuses an existing name · `append`/`patch` check the note out of the backend mirror first · `replace` needs `--confirm` on big changes |
| `blm restore <history-file> <note>` | `blm_restore` | `blm restore <note>` lists the history files |
| `blm diff <name>` · `blm merge <name> mine\|cloud\|content` | `blm_diff` · `blm_merge` | — |
| `blm conflicts [<id>] [-i] [--all]` · `blm conflict mark` · `blm resolve <note> [--keep current\|incoming] [--no-push]` | `blm_conflict {action: report\|mark, name \| content, topic, heading, reason}` · `blm_conflicts` · `blm_resolve {name, keep, push}` | `content` given = proposal for a rule (Current = block as it is, Incoming = proposal) — lands on the topic note when the topic is split out of blm.md · resolve pushes the note unless `--no-push` |
| `blm scan [path]` · `blm graph [query] [--rebuild]` | `blm_scan` · `blm_graph` | graph source: SocratiCode graph in Qdrant → ast-grep → regex · local work runs on all cores (`BLM_WORKERS` overrides) |
| `blm search "<question>" [--lang typescript] [--file p] [--exclude md] [--limit N] [--full]` · `blm search --get <search-id> --id 1,3 [--context N]` | `blm_search` | semantic search over the SocratiCode index **without its MCP** — answer is a table of contents (one line per hit: #id · score · path:lines · preview) plus a search-id, then `--get` opens only the chunks you pick with real file lines (`--full` inlines everything like codebase_search): embeds with the model/prefix the index used (`.claude/blm.json` socraticode section → env), Qdrant hybrid dense+BM25 RRF — same scores as `codebase_search` (1.0 = rank 1 on both) · `.md` under the blm store and `.agentsroom/` are always dropped, `--exclude md` drops every `.md` · Thai or English |
| `blm grep "<terms>" [--path \<dir\> \| --file \<file\>] [--max-line N] [--max-result N] [--ext .ts,.md] [--sc]` | `blm_grep` | case-insensitive substring search; snippet window = empty-line boundaries or ±maxLine/2 around hit; extensions/skip-dirs configurable; parallel 4-worker pool; --sc asks `blm search` first for candidate files |
| — | `blm` (read rules) | `/blm ["topic"]` |
| — | `blm_stat` | — |
| `blm hook session-start\|prompt\|post-edit\|grep-nudge` · `blm statusline [--top-only]` | — | one central Claude Code hook, machine-wide in `~/.claude/settings.json`: the core of `blm.md` (Main Business table + rule blocks + three standing rules) at every session start incl. after compaction, and — debounced (`BLM_CORE_INTERVAL`, default 20 min, or when blm.md changed) — before a prompt / after Write/Edit; index state at session start, top-3 `file:line` pointers per prompt (no code inlined), a nudge after Grep/Glob; statusline `socraticode : online · N nodes / M edges · ✓ synced\|⟳ indexing` (+ ctx% / session line) — `blm hook --help` prints the settings block |
| `blm update ["<topic>"] [--limit N]` | `blm_update {topic?, limit?}` | what `blm.md` covers and what it does not — the starting point of `/blm_update` (add a main topic months after `/blm_init`) without re-scanning and without an LLM: topics + dirs their `code:` lines cover · graph clusters no rule refers to · files changed since the newest rule (`no rule` / `covered — rule may be stale`) · with a topic: the `blm search` table of contents for it. Same report in the terminal and for the agent · `--html` turns it into a review page (`<store>/reviews/review-<id>.json` + `.html` on `127.0.0.1`): the owner ticks refs, presses New topic, later comments / agrees / drafts every proposed name and meaning; the agent writes its proposals into the same file (`blm_update {from, proposal}`); every click is saved, the agent is only called on submit · `--html --from <json>` re-renders · `blm update draft` lists what is still DRAFT |
| `blm self-update [--check] [--binary \| --plugin]` (also `blm --self-update`) | `blm_selfupdate` | update blm itself: binary (dev checkout → `git pull` + `go build` in place · global install → latest GitHub release replaces the file) + plugin (`claude plugin marketplace update blm` → `claude plugin update blm@blm`) · then `/mcp reconnect plugin:blm:blm` |
| `blm review get <id> · list [folder] · read <path> · write <path|id> [--file] · delete <path|id> [--confirm name]` | `blm_review {action, path, content?, confirm?}` | files of the blm store (review json/html, ignore.json…) through the blm process — the only way when the agent's Bash sandbox refuses to write `.agentsroom/blm/**` · `delete` without `confirm` never deletes (ask the owner first); deleted files go to `<store>/.trash/`; every call logged in `<store>/commands.log` · `/blm_review` = open the standing update page (`/r/update`) in the browser and go live |
| `blm guard` (hook) · `blm mcp` (server) · `blm path` · `blm init` | | `/blm_init` · `/blm_update ["topic"]` (one new main topic: candidates → meaning → subtopics → `blm-<slug>` + table row) · `/blm:blm_help` (help + how memory and blm.md are edited) |

`blm tools` manages the code-analysis neighbours the agent leans on during `/blm_init` (they cut agent token usage): `socraticode`, `obsidian`, `graphify` — `status · get · install · start · stop · restart · gen-graph`. Install logic lives in `scripts/tools.sh` (macOS/Linux) and `scripts/tools.ps1` (Windows), embedded in the binary; every install checks first and only adds what is missing.

```sh
blm init --agentsroom --tools socraticode            # bundle: configure + install missing tools
blm tools install socraticode --local                # Qdrant + Ollama + nomic-embed-text on THIS machine (mac: brew · linux: release binary + ollama.com)
blm tools install socraticode --docker               # or Docker (installs the Docker CLI if missing; Windows: docker only)
blm tools install socraticode --remote 192.168.1.50 --embedding-model bge-m3 --embedding-dimensions 1024 --embedding-context-length 8192
                                                     # services on ANOTHER machine (a GPU box on your network): nothing installed here
blm tools install socraticode --remote               # later: reuse what .claude/blm.json remembers (e.g. on a second dev machine after git pull)
blm tools update socraticode                         # latest SocratiCode plugin from its marketplace (the MCP itself already runs socraticode@latest)
blm tools status
```

`--local` writes `QDRANT_MODE/QDRANT_URL/OLLAMA_MODE/OLLAMA_URL=external/127.0.0.1` into `~/.claude/settings.json` `env` so SocratiCode uses your services instead of starting containers; docker mode removes them.
`--remote <host>` installs nothing: blm checks that Ollama (`:11434`, with the model) and Qdrant (`:6333`) answer on that host (`--ollama-url` / `--qdrant-url` for other ports), saves the values as the `socraticode` section of `.claude/blm.json`, and writes the nine SocratiCode env vars (`OLLAMA_*`, `QDRANT_*`, `EMBEDDING_MODEL/DIMENSIONS/CONTEXT_LENGTH/QUERY_PREFIX/DOCUMENT_PREFIX`) into `~/.claude/settings.json` — reconnect the socraticode plugin afterwards. A model other than nomic gets empty prefixes unless you pass `--embedding-query-prefix` / `--embedding-document-prefix`. Both modes also wire Claude Code: hooks `blm hook session-start|prompt|grep-nudge` into `~/.claude/settings.json` (only blm's own earlier entries are replaced; other tools' hooks such as graft stay and you are told how to remove them yourself) and the socraticode line at the end of `~/.claude/statusline-command.sh` (created from blm's default statusline when the file is missing; `statusLine` in settings points at it unless you already use another command) — re-running is idempotent. With no flag, `install socraticode` is `--local` when nothing is configured; when the project already has a remote section blm stops and asks you to say `--remote` (reuse) or `--local` (install here, dropping the remote section) so a local install is never started by accident; `blm status` / `blm graph` / `blm grep --sc` read the same section, so they follow the server too. Works on Windows (no script involved). Tools installed later are added to `tools` in `.claude/blm.json` automatically.

## Layout

- `cmd/blm` — entrypoint · `internal/blm` — store (checkout/base/dirty, restore), rules, linkpath, conflict/propose/resolve, diff/merge, graph (socraticode/ast-grep/regex, parallel), layout, stats, status, tools · `internal/mcp` — stdio JSON-RPC · `internal/cli` — init, guard, PATH
- `plugin/` — Claude Code plugin (manifest pointing at `blm mcp`, skill, commands) · `.claude-plugin/marketplace.json` — this repo is the marketplace · the skill is an index (`plugin/skills/blm/SKILL.md`) that links to one file per topic: [memory-notes](plugin/skills/blm/memory-notes.md) · [business-rules](plugin/skills/blm/business-rules.md) · [blm-search](plugin/skills/blm/blm-search.md) (search, score, origin) · [project-explore](plugin/skills/blm/project-explore.md) · [socraticode-remote](plugin/skills/blm/socraticode-remote.md) (local/remote stack, server setup) · [hooks-statusline](plugin/skills/blm/hooks-statusline.md) · [install](plugin/skills/blm/install.md) · [contributing](plugin/skills/blm/contributing.md) (fork → PR → release) · [working-with-owner](plugin/skills/blm/working-with-owner.md)
- `make test` · `make install` (build → `~/.blm/bin/blm`, `~/.local/bin/blm` symlinked to it — same layout as install.sh) or `make install-dev` (`~/.local/bin/blm` → this repo's `./bin/blm`) (version = latest git tag, e.g. `2.0.6` or `2.0.6-2-g33bc0b6` past the tag) · `make release` (auto-bumps the patch number from the last tag on origin; `RELEASE_VERSION=v2.1.0` to pick one) — 5 targets + GitHub Release via `gh`
