# ไม่ใส่ VERSION = เอา tag ล่าสุดที่ commit นี้สืบทอด (ดึง tag จาก origin ก่อนถ้าถึงได้) เช่น 2.0.6 หรือ 2.0.6-2-g33bc0b6 เมื่ออยู่หลัง tag
VERSION ?= $(shell git fetch -q --tags origin 2>/dev/null; git describe --tags --match 'v*' --always --dirty 2>/dev/null | sed 's/^v//')
LD = -X github.com/Ekkapap/business-logic-memory/internal/blm.Version=$(VERSION)

build:      ## bin/blm สำหรับเครื่องนี้
	go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
# การติดตั้ง (เจ้าของกำหนด 2026-09-20): repo นี้ไว้ dev/commit เท่านั้น ตัวที่ใช้จริงอยู่ที่ ~/.blm/bin/blm เหมือนทุกเครื่อง (install.sh ลงที่เดียวกัน)
#   make install      = global : build → ~/.blm/bin/blm แล้ว ~/.local/bin/blm เป็น symlink ไปที่นั่น — `blm self-update` จะแทนไฟล์ใน ~/.blm/bin
#   make install-dev  = dev    : build ./bin/blm ใน repo แล้ว ~/.local/bin/blm ชี้มาที่นี่ (agent ใน sandbox build เองได้) — `blm self-update` จะ git pull + build ที่นี่
BLM_HOME ?= $(HOME)/.blm
install: install-global
install-global:   ## build → ~/.blm/bin/blm + symlink ~/.local/bin/blm → ~/.blm/bin/blm
	mkdir -p $(BLM_HOME)/bin $(HOME)/.local/bin && GOCACHE=$${GOCACHE:-$${TMPDIR:-/tmp}/gocache} go build -ldflags "$(LD)" -o $(BLM_HOME)/bin/blm ./cmd/blm
	@ln -sfn $(BLM_HOME)/bin/blm $(HOME)/.local/bin/blm 2>/dev/null || echo "symlink not writable here — run once: ln -sfn $(BLM_HOME)/bin/blm ~/.local/bin/blm"
	@$(BLM_HOME)/bin/blm version
	@$(HOME)/.local/bin/blm path
	@echo "then in Claude Code:  /mcp reconnect plugin:blm:blm"
	@printf "/mcp reconnect plugin:blm:blm" | pbcopy 2>/dev/null && echo "(copied to clipboard — just paste and Enter)" || true
install-dev:      ## ./bin/blm + symlink ~/.local/bin/blm → ./bin/blm (dev checkout)
	mkdir -p bin && GOCACHE=$${GOCACHE:-$${TMPDIR:-/tmp}/gocache} go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
	@ln -sfn $(CURDIR)/bin/blm $(HOME)/.local/bin/blm 2>/dev/null || echo "symlink not writable here — run once: ln -sfn $(CURDIR)/bin/blm ~/.local/bin/blm"
	@./bin/blm version
	@echo "then in Claude Code:  /mcp reconnect plugin:blm:blm"
	@printf "/mcp reconnect plugin:blm:blm" | pbcopy 2>/dev/null && echo "(copied to clipboard — just paste and Enter)" || true
install-local: install-dev
test:
	go vet ./... && go test ./...
# make release                       → next patch version automatically (last remote tag +1)
# make release RELEASE_VERSION=v2.1.0 → explicit
release:
	scripts/release.sh $(RELEASE_VERSION)
.PHONY: build install install-local install-dev install-global test release
