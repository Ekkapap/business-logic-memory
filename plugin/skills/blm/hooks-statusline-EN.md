# Hooks + statusline of Claude Code for SocratiCode index (blm owns it)

**[ไทย](hooks-statusline.md)** · English

All normal Claude Code mechanics: hooks receive JSON on stdin then inject context back with `{"hookSpecificOutput":{"hookEventName":…,"additionalContext":…}}` · statusline receives JSON prints text · blm does it in one command (`blm hook …`, `blm statusline`). No scripts outside.

**The concept comes back by itself.** One central hook (`blm hook <event>`) injects the *core* of `blm.md` — the Main Business table, the list of rule blocks and three standing rules — at every session start, including right after auto-compaction, and again before a prompt or after edits **only when a debounce has expired** (20 min by default, or when `blm.md` changed), so an agent editing fifty files in a row is not re-briefed fifty times. `/blm` lets the owner check at any moment whether the agent and the rules still agree.

## What gets injected to agent

| event | command | does |
|---|---|---|
| `SessionStart` (startup/resume/clear/**compact**) | `blm hook session-start` | **core brief** of blm.md always (after auto-compact context comes back here, line says just-compacted) + index status + nudge to ask `blm_search` before grep |
| `UserPromptSubmit` | `blm hook prompt` | before you act: core brief **past debounce** + prompt ≥ 12 chars → pointer 3 lines `path:Lstart-Lend (score)` no code attached (same prompt reused, don't search again) |
| `PostToolUse` matcher `Write\|Edit\|MultiEdit` | `blm hook post-edit` | core brief **past debounce only** — long job editing files rapid-fire, no new prompt still gets heart of rules as interval, not every time |
| `PostToolUse` matcher `Grep\|Glob` | `blm hook grep-nudge` | warn index exists — "where is / how does" should use `blm_search` |

**core brief** = the core of `blm.md` (Main Business table + rule blocks + three standing rules) — pulled from store not whole file. Injected at every session start incl. after compaction, and — debounced (`BLM_CORE_INTERVAL`, default 20 min, or when `blm.md` changed) — before a prompt / after Write/Edit. Index state at session start, top-3 `file:line` pointers per prompt (no code inlined), a nudge after Grep/Glob.

**debounce** (state per session at `$TMPDIR/blm-hook-<session_id>.json`): inject again past `BLM_CORE_INTERVAL` minutes (default 20; `0` = every time) or blm.md changed (hash) · `session-start` injects always. · PreToolUse can't inject (Claude Code takes allow/deny only) so uses prompt = before acting, post-edit = during.

Stack down / no index / no blm.md / error = silent exit 0. Hook doesn't break session. · project = `CLAUDE_PROJECT_DIR` → `cwd` in JSON → cwd.

## Statusline

`socraticode : online · 4213 nodes / 24326 edges · ✓ synced` (+ line 2 `▸ ctx 22% · session: 878f733d` except `--top-only`).

- `online` = Ollama **and** Qdrant answer (search needs both) · nodes/edges from single point in `<projectId>_symgraph_meta` of Qdrant.
- `✓ synced` = index up to code · `⟳ indexing` = files the index sees changed after last round, socraticode's watcher catching up (check mtime of files in `git status` not ignored by `.socraticodeignore` — let git assess ignore via `core.excludesFile`) · `not indexed` = no graph for this project yet · offline shows `indexed dd/mm hh:mm` latest.
- Result from server cached 10s in temp (`blm-socraticode-stats-<projectId>.json`) because statusline called every tick.

## Wire (blm does it at `blm tools install socraticode` both --local/--remote — idempotent)

- `~/.claude/settings.json` `hooks`: put 4 entries above. · **unplug blm's own** (`blm hook …` old version, `socraticode-hooks.cjs`). Other tools' hooks stay complete. · See graft will print `note: graft hooks still installed — remove with: graft uninstall -y`.
- `~/.claude/statusline-command.sh`: exists → replace only block in marker `# >>> blm socraticode statusline >>>` … `# <<< … <<<` append new. · doesn't exist → write default script baked in binary (time · folder · git branch/dirty/worktree · model · ctx vs. auto-compact + socraticode line).
- `statusLine`/`subagentStatusLine` in settings: set when missing or point at old `socraticode-statusline.cjs`. · Points elsewhere (graft/yours) don't touch. Just note.

Manual (machine that didn't go through install): `blm hook --help` prints settings.json block and statusline line to copy.

## Test

```sh
echo '{"cwd":"'$PWD'","prompt":"how does cookie consent work"}' | blm hook prompt
echo '{"cwd":"'$PWD'","session_id":"x","context_window":{"used_percentage":21}}' | blm statusline
```
