# SocratiCode + LLM + Qdrant — local or remote install and wire to Claude Code

**[ไทย](socraticode-remote.md)** · English

SocratiCode = Claude Code plugin (`giancarloerra/socraticode`) that indexes code into chunks at function boundaries (tree-sitter via ast-grep) → embeds with **Ollama** → stores in **Qdrant** and builds symbol/call graph. · Search **needs both services online** (question always embedded first, no keyword-only fallback). · blm uses this index via `blm_search` / `blm tools status` / `blm graph` without going through its MCP.

## Choose a mode

| | `--local` (old) | `--remote <host>` (new 2026-09-20) |
|---|---|---|
| Ollama + Qdrant | On this machine (mac: qdrant binary + launchd, brew ollama · linux: binary + ollama.com · windows: `--docker` only) | On another machine you already set up (GPU box, server in VPN) — this machine installs nothing |
| Model | `nomic-embed-text` 768 dimensions (socraticode default) | You specify, e.g., `bge-m3` 1024 dimensions (Thai support, context 8192) |
| Dev machine RAM | Qdrant ~900 MB idle + Ollama loads model on use | 0 |
| Store at | `~/.claude/settings.json` env | `.claude/blm.json` section `socraticode` + env |

```sh
blm tools install socraticode --local
blm tools install socraticode --remote 192.168.1.50 --embedding-model bge-m3 --embedding-dimensions 1024 --embedding-context-length 8192
blm tools install socraticode --remote        # second machine after git pull: use values from .claude/blm.json
blm tools install socraticode                 # no mode: no remote saved = --local · has remote = stop and ask
blm tools status socraticode                  # running · model=… · remote=… · ollama=up · qdrant=up
blm tools update socraticode                  # latest plugin from marketplace (its MCP already runs npx socraticode@latest)
```

Both modes do at the end: write 9 env vars (`OLLAMA_MODE/URL`, `QDRANT_MODE/URL`, `EMBEDDING_MODEL/DIMENSIONS/CONTEXT_LENGTH/QUERY_PREFIX/DOCUMENT_PREFIX`) to `~/.claude/settings.json` · wire hooks `blm hook session-start|prompt|grep-nudge` and socraticode line in `~/.claude/statusline-command.sh` (see [hooks-statusline-EN.md](hooks-statusline-EN.md)) · unplug blm's own pieces, other tools' pieces (e.g., graft) stay complete with notes on how to unplug. · end with `/plugin` → reconnect socraticode → `codebase_health` must show both external → `codebase_index` — if this project's collection isn't on Qdrant yet now.

## Embedding models

- Query and index **must be the same model** (same space). Change model = reindex the whole repo.
- `nomic-embed-text` English mainly, prefix `search_query: ` / `search_document: ` · `bge-m3` Thai support, no prefix (empty both), context 8192 · `multilingual-e5-large` context only 512 (2000-char chunks get cut), not recommended.
- Models socraticode doesn't know (bge-m3): specify `--embedding-dimensions` + `--embedding-context-length` yourself (`MODEL_CONTEXT_LENGTHS` in embedding-config.ts missing).
- `blm search` reads model/prefix from `.claude/blm.json` first → env → `~/.claude/settings.json` → 127.0.0.1.

## Set up server side (remote) — example Windows Server + GPU

Do in PowerShell as Administrator one block at a time. · `<host-ip>` = IP the dev machine can reach (VPN/tailnet). Bind services to that IP, not `0.0.0.0`.

1. NVIDIA driver → `nvidia-smi` sees the card.
2. Ollama: `irm https://ollama.com/install.ps1 | iex` (complain about signature first time, rerun or download `OllamaSetup.exe` run `/VERYSILENT`) → `ollama pull bge-m3` → test `POST /api/embed` returns 1024 and `ollama ps` = `100% GPU`.
3. **Run as service not tray app** (`ollama app.exe` = GUI) with NSSM:
   ```powershell
   C:\nssm\nssm.exe install Ollama "$env:LOCALAPPDATA\Programs\Ollama\ollama.exe" serve
   $m = "$env:USERPROFILE\.ollama\models"
   C:\nssm\nssm.exe set Ollama AppEnvironmentExtra "OLLAMA_HOST=<host-ip>:11434" "OLLAMA_KEEP_ALIVE=24h" "OLLAMA_CONTEXT_LENGTH=8192" "OLLAMA_MODELS=$m"
   C:\nssm\nssm.exe start Ollama
   ```
   `OLLAMA_MODELS` must point to the folder where you pulled models (service runs as SYSTEM, sees different home).
4. Qdrant: `qdrant-x86_64-pc-windows-msvc.zip` from GitHub release → `C:\qdrant` · `config\config.yaml`: `service.host: <host-ip>`, `http_port: 6333`, `storage.storage_path: C:\qdrant\storage` → NSSM service `Qdrant` (`qdrant.exe --config-path …`) → `GET /collections` answers.
5. Firewall: inbound TCP 11434 and 6333 only to VPN IP range (`-RemoteAddress 100.64.0.0/10` for tailnet). Don't open public — both services have no auth (`QDRANT_API_KEY`/`OLLAMA_API_KEY` second layer).
6. VPN client on server if using headscale/tailscale: MSI quiet `msiexec /i tailscale-setup-<ver>-amd64.msi /quiet TS_NOLAUNCH=1 TS_UNATTENDEDMODE=always TS_LOGINURL=<control-server>` then `tailscale up --login-server <control-server> --authkey <key>`.
7. From dev machine: `curl http://<host-ip>:11434/api/tags` and `curl http://<host-ip>:6333/` answer → `blm tools install socraticode --remote <host-ip> …`.

Linux/macOS server side: `ollama serve` with `OLLAMA_HOST=<host-ip>:11434` as systemd/launchd + qdrant binary from release (same as `blm tools install socraticode --local` does on this machine) — single ssh command for all three yet to do.

## Gotchas hit for real

- Change env in `~/.claude/settings.json` then MCP still uses old value — reads only at start. Must `/plugin` reconnect.
- `blm search`/statusline in agent sandbox: `EPERM`/x509 can't reach server even though curl works outside — sandbox proxy; real results from owner's terminal.
- Ollama down = can't search, not just no indexing. · Qdrant down = index/graph/hash gone from all angles (all live in Qdrant).
- Forgot `--remote` and just `install socraticode` on project that remembers remote → blm stops with tip. Won't install local over it.
