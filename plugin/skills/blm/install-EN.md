# Install blm — binary + plugin · update · file locations

**[ไทย](install.md)** · English

**One plugin, self-updating.** `blm init` installs the binary, the Claude Code plugin and the hooks; `blm self-update` refreshes both. CLI and MCP share one code path, so what the owner sees in the terminal is what the agent sees.

blm has two parts that go together: **binary `blm`** on PATH (CLI + MCP server `blm mcp`) and **plugin `blm@blm`** in Claude Code (skill + slash commands + `.mcp.json` that points to `blm mcp`) — plugin without binary can't start MCP.

## 1. Install binary (once per machine) — run from root of the project blm should remember

```sh
# macOS / Linux — into ~/.blm/bin/blm + symlink ~/.local/bin/blm then blm init
curl -fsSL https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.sh | sh -s -- --agentsroom

# Windows (PowerShell) — into ~\.blm\bin\blm.exe + PATH (User)
$env:BLM_INIT_ARGS="--agentsroom"; irm https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.ps1 | iex

# Have Go
go install github.com/Ekkapap/business-logic-memory/cmd/blm@latest && blm path && blm init --agentsroom

# Developer (checkout of repo): make install = build → ~/.blm/bin/blm · make install-dev = ~/.local/bin/blm → ./bin/blm of repo
```

The installer puts the binary at `~/.blm/bin/blm` (Windows: `~\.blm\bin\blm.exe`) and links `~/.local/bin/blm` to it; `blm self-update` replaces that file from the latest GitHub release. Developers: `make install` builds into the same `~/.blm/bin` (the repo stays a clean checkout for commits), `make install-dev` links `~/.local/bin/blm` to the repo's `./bin/blm` instead, in which case `blm self-update` does `git pull` + `go build` there.
- **backend** (where memory's central store is) pick at init and change later by running init again:

  | flag | store | sync |
  |---|---|---|
  | `--agentsroom` **(recommended)** | `.agentsroom/blm` + mirror `.agentsroom/memory` | push = `memory_save` via AgentsRoom MCP (https://agentsroom.dev) · pull = refresh mirror — local first, sync later, how blm designed |
  | *(unspecified)* | self-detect: have `.claude/blm.json` → keep · `.agentsroom/` → agentsroom · `.obsidian/` → obsidian · else `none` | per detected backend |
  | `none` | `.claude/blm` | **no sync** — files are the truth (`blm_sync` not declared) |
  | `--obsidian` | `blm/` (visible in vault) | no sync |
  | `--dir <p> --backend <cli>` | `<p>` | agent learns push/pull commands from `<cli> --help` at `/blm_init` then stores as template in `.claude/blm.json` |

- `blm init` refuses outside project folder (no `.git`/`.agentsroom`/`package.json` …). `--force` skips. · unspecified backend: have `.claude/blm.json` → keep · `.agentsroom/` → agentsroom · `.obsidian/` → obsidian · else none.
- init writes `.claude/blm.json`, store, `.ignorememory`, merges `.claude/settings.json` (deny Edit/Write store, `blm guard` PreToolUse, sandbox denyWrite), wires hooks at machine level in `~/.claude/settings.json` (`blm hook session-start / prompt / grep-nudge / post-edit`), adds `blm` to PATH (or prints command if can't), registers Claude Code plugin. · Rerunnable (idempotent). · `--tools socraticode,tree-sitter` install neighboring tools not yet there.

## 2. Install plugin in Claude Code

`blm init` does it already (via `claude plugin marketplace add` + `claude plugin install`) — do manually when init can't or machine didn't have `claude` then:

```sh
# marketplace = this repo itself (.claude-plugin/marketplace.json) · plugin name blm
claude plugin marketplace add Ekkapap/business-logic-memory
claude plugin install blm@blm
```

Or in Claude Code: `/plugin` → Marketplaces → add `Ekkapap/business-logic-memory` → install `blm` · marketplace from local checkout: `blm init --marketplace <dir>` or `claude plugin marketplace add /path/to/business-logic-memory`.

Then `/reload-plugins` (or reopen Claude Code) → `/blm:blm_help` must answer.

## 3. Check it works

```sh
blm version                 # tag e.g., 2.0.8 (or 2.0.8-3-g… = build after tag)
blm status                  # READY / NOT READY <why> first line
```

In Claude Code: tools `blm_*` must show up (`blm`, `blm_create`, `blm_search`, `blm_tools`, `blm_selfupdate` …) · not there = `/mcp reconnect plugin:blm:blm`.

## 4. Update

```sh
blm self-update             # binary (dev checkout → git pull + go build in place · global install → latest GitHub release replaces the file) + plugin (claude plugin marketplace update blm → claude plugin update blm@blm)
blm self-update --check     # look only
```

MCP: `blm_selfupdate {check?, binary?, plugin?}` · after update `/reload-plugins` (the session still holds the old plugin: commands, skills, version) then `/mcp reconnect plugin:blm:blm` (running MCP still old binary till you reconnect). · plugin version matches binary since the release where `make release` bumps `plugin.json`.

## 5. Uninstall

```sh
claude plugin uninstall blm@blm
rm -rf ~/.blm ~/.local/bin/blm            # Windows: %USERPROFILE%\.blm
```

In project: `.claude/blm.json`, store (`.agentsroom/blm/` or `.claude/blm/`), lines `blm guard`/deny in `.claude/settings.json` — delete by hand (init writes as merge, uninstall as merge too, nothing auto).
