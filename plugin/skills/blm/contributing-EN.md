# Develop blm further — fork · build · test · send back (pull request)

**[ไทย](contributing.md)** · English

Repo: https://github.com/Ekkapap/business-logic-memory (public, Go only, no dependencies outside stdlib).

## 1. Prep machine

- Go ≥ 1.25 (`go.mod`) · git · `make` · Claude Code CLI (`claude`) to test plugin · `gh` for release.
- Don't need Ollama/Qdrant to build/test (test mocks with `httptest`) — need only to try `blm search` against real index.

## 2. Fork + clone

```sh
gh repo fork Ekkapap/business-logic-memory --clone      # or click Fork on GitHub then
git clone git@github.com:<you>/business-logic-memory.git
cd business-logic-memory
git remote add upstream https://github.com/Ekkapap/business-logic-memory.git
```

## 3. Build to use on yourself during dev

```sh
make install-dev      # build ./bin/blm then ~/.local/bin/blm → ./bin/blm of this checkout (blm self-update will git pull + build here)
make install          # build → ~/.blm/bin/blm like normal user (test install/self-update as release)
```

After build each time in Claude Code: `/mcp reconnect plugin:blm:blm` (running MCP still old binary till you reconnect) · `make install-dev` copies the command to clipboard on mac.

Test plugin from checkout without release: `claude plugin marketplace add /path/to/business-logic-memory` then `claude plugin install blm@blm` (marketplace = `.claude-plugin/marketplace.json` of repo).

## 4. Code layout

- `cmd/blm/main.go` — CLI: usage, parse flags (`splitFlags`), dispatch to `mcp.Server.Call` (CLI and MCP use same code), help per command (`searchHelp`, `hookHelp`, `updateHelp` …).
- `internal/mcp/server.go` — declare tools (`obj/str/enum` schema) + `Call` switch.
- `internal/blm/` — logic: store/checkout/history (`store.go`), rules + linkpath, conflict/propose/resolve, diff/merge, sync with AgentsRoom (`agentsroom.go`), graph (socraticode/ast-grep/regex), grep/cat, **search + trust** (`search.go`, `trust.go`), hooks/statusline (`hook.go`), tools install/wire (`tools.go`, `wire.go`, `scripts/tools.sh|ps1|statusline-command.sh` baked in binary), self-update (`selfupdate.go`), layout/colors (`layout.go`), config (`config.go`).
- `internal/cli/` — `init`, `guard` (PreToolUse hook), PATH.
- `plugin/` — Claude Code plugin: `.claude-plugin/plugin.json`, `skills/blm/` (SKILL.md + sub-files you're reading), `commands/*.md` (slash).
- Tests next to files (`*_test.go`) — network mocked with `httptest`, HOME/TMPDIR/BLM_CACHE_DIR point to temp always, never touch real `~/.claude`.

## 5. Rules when editing

- **Before adding tool / flag / new file to blm, tell owner one line**: what you'll add and whether the old thing can extend instead (lesson 2026-09-09: `blm_propose` doubled `blm_conflict` which takes `content`).
- Comments explain **why** (Thai ok) and date/who when it's an owner-decided rule. · No `any`-style shortcuts, don't swallow errors silently except hooks/statusline that intentionally stay quiet (comment says so).
- CLI and MCP must give same result (CLI calls `srv.Call` then renders to `terminal`). · Add tool = fix both `server.go` (schema+Call), `main.go` (usage+case+help), `plugin/commands/*.md`, README table row, affected skill file.
- `gofmt` all touched files. · `make test` (= `go vet ./... && go test ./...`) must pass.
- In agent sandbox: `GOCACHE=$TMPDIR/gocache GOMODCACHE=$TMPDIR/gomod go test ./...` (real cache unwritable). · `blm guard` catches messages with store path in Bash commands — patch script into file and run instead of heredoc.

## 6. Commit · push · pull request

```sh
git checkout -b feat/<topic>
# … edit · gofmt · make test
git add <files-edited>            # not .agentsroom/ .claude/ bin/
git commit -m "feat: <what> — <why/result>"
git push -u origin feat/<topic>
gh pr create --base main --repo Ekkapap/business-logic-memory --title "feat: …" --body "…"   # or click Compare & pull request on GitHub
```

- Commit format: `feat:` / `fix:` / `chore:` / `docs:` one-line subject saying both "what" and "why" · body as bullets can continue topic.
- PR body: problem → what changed → how you tested (commands run + actual results) · if CLI/MCP changed, paste example output.
- Sync with upstream first: `git fetch upstream && git rebase upstream/main`.
- Don't push to upstream `main` directly (owner only). · release (`make release`) owner does: bump `plugin.json` → commit+push → tag `vX.Y.Z` → build 5 platforms → GitHub Release.

## 7. Release (owner)

```sh
make release                       # next patch from latest tag on origin
make release RELEASE_VERSION=v2.1.0
```

`scripts/release.sh`: write `version` in `plugin/.claude-plugin/plugin.json` = tag → commit `chore: plugin version` → `git push origin HEAD` → tag + push → build `dist/blm_<os>_<arch>.tar.gz|zip` (darwin/linux/windows) → `gh release create`. · Users get both binary and plugin same version through `blm self-update`.
