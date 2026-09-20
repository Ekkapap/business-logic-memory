# Record memory between sessions — three places · write commands · sync

**[ไทย](memory-notes.md)** · English

## Three places, call them straight (named by owner 2026-09-09)

- **blm.md / notes in blm/** = `<store>/` (agentsroom backend: `.agentsroom/blm/`) where work happens. Notes touched for the first time are pulled from backend with base state. Status per note: `synced` / `edited`. After pushing, the file stays and base moves forward.
- **mirror** = `.agentsroom/memory/`. Copy of AgentsRoom written by the app. Used only to see who's newer. Not a place to edit.
- **AgentsRoom** = cloud endpoint. Changes only when you push.
- File count on both sides doesn't have to match (cloud has knowledge/how-to that isn't business logic · blm/ has drafts, reports, graphs). The one thing that must exist on both sides is blm.md.
- Other backend: `none` = store in `.claude/blm`. Files are the truth. No sync. · `obsidian` = `blm/` folder in vault. · `custom` = push/pull commands learned from `<cli> --help` at init.

## Write memory (agent does this, no waiting for anyone)

- `blm_create {name, content, description, folder, target, mode}`. New notes only. Duplicate name = rejected. · `folder` = `features` | `features/<x>` | `global/architecture|conventions|pitfalls` (unspecified = guessed from mirror) · `mode` = `append` (append to existing backend note) | `replace` (whole new note, requires description).
- `blm_append {name, content}` append. · `blm_patch {name, find, replace}` search/replace, match exactly 1. Both pull the note from mirror into blm/ first if not there.
- `blm_replace {name, content, confirm}` overwrite the whole file. Must exist. Different enough (size >30% or >30% of original lines lost): blm returns `needsConfirm` with numbers and lines that will be lost. Call again with `confirm:true` only if intentional. **Never use on blm.md**.
- **Never `memory_save`/`memory_get` directly** (note content runs through context and overwrites wholesale). Edit/Write/Bash writing to blm/ is denied. All writes through blm have history: `history/<name>-[action]-YYYYMMDD-HHmmss.md`. Restore with `blm_restore {name, history}` (`blm restore <file> <note>`; `blm restore <note>` lists).
- **Record right away when proven** not at the end of the job — session compact and what was only in chat disappears (lesson 2026-09-09).
- **Owner points out a problem in blm.md** (wrong link, typo) = read that line for real (`grep -n` in store is readable), then `blm_patch` right away. **Don't answer that sandbox won't let you edit** — that restriction only applies to blm's Go source.
- Links in notes/blm.md: `[filename](path from repo root)`. Don't wrap in `<>`. Escape `[ ]` in names. `linkPath` does it at checkout and during `blm conflict mark`.

## Sync (agentsroom/custom backend only)

- Owner says "update memory" → `blm_sync {apply:true, author, role}`. blm spawns AgentsRoom MCP itself, fires `memory_save` one by one, checks with `memory_list` once. Notes matching base aren't sent. No auto-retry. Note content doesn't go through context. Cloud edited after base → no overwrite. Returns `conflict:` → go to `blm_diff` → `blm_merge {keep}` or file `blm_conflict`.
- `blm_sync {direction:"pull", apply:true}` when you think cloud changed: mirror fresh, then notes you haven't edited and cloud is newer → pull and overwrite. · Edited locally + cloud changed → report as conflict, don't overwrite.
- `names:[…]` limit to specific notes only. · `delete:[…]` delete notes from backend after push (e.g., old name after rename).
- Don't fire `memory_save` yourself instead of this step unless `blm_sync` says no AgentsRoom MCP found in `.mcp.json`.
- Workaround when a draft `replace` collides with cloud that's newer without sending the whole note through context: `blm_merge keep=cloud` → `blm_sync pull apply` → `blm_append` new → push.
