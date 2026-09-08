# blm — business-logic-memory

One binary. Session temp memory for AI agents + a single business-logic source of truth (`blm.md`) they test themselves against.

- **Jot fast during the session** — `blm_save / blm_update / blm_patch`: local files, milliseconds, never blocks the agent while a slow memory backend (AgentsRoom, a wiki, a CLI) is synced once at the end.
- **One rules file `blm.md`** — what is true *now*, owned by the human. Each subtopic carries `memory:` `code:` `verify:` `updated_at:` refs. Agents read it (`blm`) whenever memory conflicts with code.
- **Self-test report** — `/blm ["topic"]`: the agent writes what it believes *before* reading, then reports PASSED / NOT PASSED / UNKNOWN per subtopic with clickable `blm.md:<line>` refs. Columns are aligned by real monospace width (Thai combining vowels = 0, emoji = 2).
- **Stats like `rtk gain`** — how often the agent was wrong, how often the rules file pulled it back, how often the human had to change a rule.
- **History** — every change/delete snapshots the old file to `history/<name>-[action]-YYYYMMDD-HHmmss.md`.
- **Write lock** — Edit/Write denied, `blm guard` PreToolUse hook, optional OS sandbox `denyWrite`. Only `blm` writes the store.
- **Cross-platform** — Go binary for macOS / Linux / Windows. Users call `blm status` directly; agents call the same code through MCP (`blm mcp`).

## Install (one line, inside your project)

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.sh | sh -s -- --agentsroom
# Windows (PowerShell)
$env:BLM_INIT_ARGS="--agentsroom"; irm https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.ps1 | iex
# Go toolchain
go install github.com/Ekkapap/business-logic-memory/cmd/blm@latest && blm path && blm init --agentsroom
# Homebrew (tap coming)
```

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
| `blm sync --push \| --pull` | `blm_sync` | — |
| `blm get/save/update/patch/delete` | `blm_get/save/update/patch/delete` | — |
| — | `blm` (read rules) | `/blm ["topic"]` |
| — | `blm_stat` | — |
| `blm guard` (hook) · `blm mcp` (server) · `blm path` · `blm init` | | `/blm_init` |

`blm tools` manages the code-analysis neighbours the agent leans on during `/blm_init` (they cut agent token usage): `socraticode`, `obsidian`, `graphify` — `status · get · install · start · stop · restart · gen-graph`. Install logic lives in `scripts/tools.sh` (macOS/Linux) and `scripts/tools.ps1` (Windows), embedded in the binary; every install checks first and only adds what is missing.

```sh
blm init --agentsroom --tools socraticode            # bundle: configure + install missing tools
blm tools install socraticode                        # later: native Qdrant + Ollama (mac: brew · linux: release binary + ollama.com)
blm tools install --docker socraticode               # or Docker (installs the Docker CLI if missing; Windows: docker only)
blm tools status
```

Native mode writes `QDRANT_MODE/QDRANT_URL/OLLAMA_MODE/OLLAMA_URL=external/localhost` into `~/.claude/settings.json` `env` so SocratiCode uses your services instead of starting containers; docker mode removes them. Tools installed later are added to `tools` in `.claude/blm.json` automatically.

## Layout

- `cmd/blm` — entrypoint · `internal/blm` — store, rules, layout, stats, status, tools · `internal/mcp` — stdio JSON-RPC · `internal/cli` — init, guard, PATH
- `plugin/` — Claude Code plugin (manifest pointing at `blm mcp`, skill, commands) · `.claude-plugin/marketplace.json` — this repo is the marketplace
- `make test` · `make install` · `make release VERSION=v2.0.0` (5 targets + GitHub Release via `gh`)
