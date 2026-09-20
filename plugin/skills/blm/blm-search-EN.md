# blm_search — search code by meaning, then read the results right

**[ไทย](blm-search.md)** · English

## What it is

`blm_search` (CLI `blm search`) searches through SocratiCode's index **without going through its MCP**: embeds the question with the same model/prefix the index uses (read from `.claude/blm.json` section socraticode → env → `~/.claude/settings.json`), then Qdrant hybrid = meaning (dense) + exact words (BM25) combined with RRF — same method as `codebase_search` in every way. Gets same results and scores. · Different: blm **adds 3 things** that socraticode doesn't: table of contents instead of 10 chunks of code, filters `.md` that isn't code, and **origin** column (how much to trust).

Ask in sentences. Thai or English. Don't need to know symbol names: `"do users who sign in with LINE need to enter OTP again"` · `"cookie consent policy version localStorage"`.

## Result = table of contents, 2 lines per hit

```
search "does user need to confirm email before signing in" — 4 hits (model bge-m3 · score: RRF, 1.0 = top of both semantic and keyword)
  id   score  path:lines                                     origin
  #1   0.50   src/lib/auth/verify-email.ts:L20-L58           code 0.62
       [typescript]  export type VerifyIntent = "signup" | "login";
  #2   0.50   src/app/api/auth/login/route.ts:L30-L64        both ✓ 0.74
       [typescript]  export async function POST(request: Request): Promise<Response> {
  #3   0.45   src/features/Login/index.tsx:L40-L85           comment ~ 0.67
       [typescript]  export function LoginForm({
search-id: 1n7wzapf   open a hit: blm search --get 1n7wzapf --id 1,3
```

- Line 1: `#id` · `score` · `path:Lstart-Lend` · `origin`. · Line 2: `[lang]` + preview (first line with content in the code section, skip import/comment/`}`).
- **Doesn't carry chunks** — MCP response is light. Open only what you pick, with `get`.

## Read further only what you pick — `get`

- `blm_search {get:"1n7wzapf", ids:"1,3", context:5}` / `blm search --get 1n7wzapf --id 1,3 --context 5` → **real lines from file** (start..end ± context with line numbers). Not cached. File gone, then storage is used.
- Result file at `<store>/tmp/search-result-<id>.json` (same place as `blm grep`). Holds content of chunk + all origin data.
- `full:true` / `--full` = attach content for every chunk at once (like old `codebase_search`). Use when results are few and you need all of them. · `--brief` = pointers only, no file saved (hook uses this).

## Score — RRF from Qdrant (same scale as `codebase_search`)

`1/(2+rank)` per side, sum both sides: **1.0** = rank 1 on both semantic and keyword · **0.5** = rank 1 on one side, doesn't place on the other · 0.83 = ½+⅓ · 0.45 = ¼+⅕ · below `minScore` 0.10 cut. · **measured by rank** (file in top-3?) not by the number — owner sets. · Thai written together is one token for BM25, so gets score from semantic side mainly.

## Origin — how much to trust this result (blm analyzes itself)

SocratiCode's chunks split on function/class boundaries, so **function header comment and code are in one chunk**. blm splits them, then embeds both with the same model (1 request per search) → compares with question (`codeSim` / `commentSim`) and with each other (`agree` = cosine(code, comment) = digit at end of badge).

| Badge | Means | What to do |
|---|---|---|
| `code` | Question hits **code** directly (or chunk has no comment) | Trust it |
| `both ✓ 0.74` | Hits both code and comment, both point the same way (agree ≥ 0.60) | Trust fully |
| `both 0.55` | Hits both, but comment and code don't quite match | Read the code |
| `comment ~ 0.67` | Hits **comment only** but comment tells the same story as code | Read code to verify once |
| `comment ⚠ 0.52` | Hits comment only, code in chunk tells a different story | **Comment may be stale**. Don't trust comment. Read code. · If code contradicts comment for real, tell owner there's conflicting comments. Don't fix silently |
| `doc` | File that tree-sitter doesn't know (.md .sql .json .yaml .css .html …). No code to compare | Read as-is: spec/plan/schema |

- Comment-only chunk (header block) = `comment ⚠` always. · Chunk starting mid-block `/* … */` (socraticode can cut mid-block): split correctly.
- Thresholds: `trustMargin` 0.08 (differ more = one side wins) and `trustAgree` 0.60 default. Tune in `internal/blm/trust.go` when you've measured for real. · `noTrust:true` / `--no-trust` skip (saves 1 request).

## What files are in results — `.md` `.sql` and `db/schema/`

- **`.md` under blm store and `.agentsroom/`** (notes, memory mirror) cut **always** — memory isn't code. Find memory with `blm` / `blm grep`. · Other `.md` (`.planning/`, `design/`, README) still in results as `doc` because specs must be found. · `excludeMd:true` / `--exclude md` cut all `.md` in project. · `lang:"typescript"` (= .ts+.tsx) / `go` / `markdown` get one language.
- **SQL: index only `db/schema/<table>.sql`** — snapshot of current schema from DEV (project script generates after migrate every time — command name varies by project, e.g., `db:schema`). One file per table: columns/default/null, constraint, index, trigger, `comment on column` · migration (`db/postgres/*.sql`) and dump **not indexed**: migration is history, many files tell the same table at different times, search can't tell which one is still current. · dump is data (PII). · `.socraticodeignore`: `*.sql` except `!db/schema/*.sql`.
- Rule: dev ⊇ prod. Snapshot from DEV only. Same script with `--prod` (read-only) to compare what waits to deploy and alert if prod has something dev doesn't.

## Order of operations

1. `blm_search {query}` → see origin of top-3.
2. `get` only what you trust or need to verify (not `full:true` all 10 if you don't know which is right).
3. Then edit. · Grep/Glob when you know the symbol name for sure (hook `grep-nudge` warns if you grep while index exists).
4. No results / error → say `blm tools status socraticode` = stack down or not indexed yet → tell owner, don't retry.
