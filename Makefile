# ไม่ใส่ VERSION = เอา tag ล่าสุดที่ commit นี้สืบทอด (ดึง tag จาก origin ก่อนถ้าถึงได้) เช่น 2.0.6 หรือ 2.0.6-2-g33bc0b6 เมื่ออยู่หลัง tag
VERSION ?= $(shell git fetch -q --tags origin 2>/dev/null; git describe --tags --match 'v*' --always --dirty 2>/dev/null | sed 's/^v//')
LD = -X github.com/Ekkapap/business-logic-memory/internal/blm.Version=$(VERSION)

build:      ## bin/blm สำหรับเครื่องนี้
	go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
# สองแบบ (เจ้าของกำหนด 2026-09-09):
#   make install          = local  : build ./bin/blm ใน repo แล้วให้ ~/.local/bin/blm เป็น symlink มาที่นี่ — agent ที่ sandbox เขียน ~/.local/bin ไม่ได้ก็ build เองได้
#   make install-global   = global : build ไปแทนที่ ~/.local/bin/blm ตรง ๆ (แบบเดิม ไม่พึ่ง repo)
install: install-local
install-local:    ## ./bin/blm + symlink ~/.local/bin/blm → ./bin/blm
	mkdir -p bin && GOCACHE=$${GOCACHE:-$${TMPDIR:-/tmp}/gocache} go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
	@if [ -L $(HOME)/.local/bin/blm ] || [ ! -e $(HOME)/.local/bin/blm ]; then \
		mkdir -p $(HOME)/.local/bin 2>/dev/null; ln -sfn $(CURDIR)/bin/blm $(HOME)/.local/bin/blm 2>/dev/null || echo "symlink not writable here — run once: ln -sf $(CURDIR)/bin/blm ~/.local/bin/blm"; \
	else \
		echo "~/.local/bin/blm is a real file (global install) — switch to local once with: ln -sf $(CURDIR)/bin/blm ~/.local/bin/blm"; \
	fi
	@./bin/blm version
	@echo "then in Claude Code:  /mcp reconnect plugin:blm:blm"
	@printf "/mcp reconnect plugin:blm:blm" | pbcopy 2>/dev/null && echo "(copied to clipboard — just paste and Enter)" || true
install-global:   ## แทนที่ ~/.local/bin/blm ด้วยไฟล์จริง แล้วเติม PATH
	mkdir -p $(HOME)/.local/bin && go build -ldflags "$(LD)" -o $(HOME)/.local/bin/blm ./cmd/blm && $(HOME)/.local/bin/blm path
test:
	go vet ./... && go test ./...
# make release                       → next patch version automatically (last remote tag +1)
# make release RELEASE_VERSION=v2.1.0 → explicit
release:
	scripts/release.sh $(RELEASE_VERSION)
.PHONY: build install install-local install-global test release
