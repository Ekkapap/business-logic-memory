# ไม่ใส่ VERSION = เอา tag ล่าสุดที่ commit นี้สืบทอด (ดึง tag จาก origin ก่อนถ้าถึงได้) เช่น 2.0.6 หรือ 2.0.6-2-g33bc0b6 เมื่ออยู่หลัง tag
VERSION ?= $(shell git fetch -q --tags origin 2>/dev/null; git describe --tags --match 'v*' --always --dirty 2>/dev/null | sed 's/^v//')
LD = -X github.com/Ekkapap/business-logic-memory/internal/blm.Version=$(VERSION)

build:      ## bin/blm สำหรับเครื่องนี้
	go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
# build ลง ./bin/blm (ใน repo) แล้วให้ ~/.local/bin/blm เป็น symlink มาที่นี่ — agent ที่ถูก sandbox เขียน ~/.local/bin ไม่ได้จึง build เองได้ (เจ้าของเลือก 2026-09-09)
# symlink สร้างครั้งแรกอัตโนมัติ · ถ้ามีไฟล์จริงอยู่ (ติดตั้งแบบเก่า) จะบอกให้เจ้าของแทนด้วย ln -sf เอง
install:    ## ./bin/blm + symlink ~/.local/bin/blm → ./bin/blm
	mkdir -p bin && GOCACHE=$${GOCACHE:-$${TMPDIR:-/tmp}/gocache} go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
	@if [ -L $(HOME)/.local/bin/blm ] || [ ! -e $(HOME)/.local/bin/blm ]; then \
		mkdir -p $(HOME)/.local/bin 2>/dev/null; ln -sfn $(CURDIR)/bin/blm $(HOME)/.local/bin/blm 2>/dev/null || echo "symlink not writable here — run once: ln -sf $(CURDIR)/bin/blm ~/.local/bin/blm"; \
	else \
		echo "~/.local/bin/blm is a real file — replace it once with: ln -sf $(CURDIR)/bin/blm ~/.local/bin/blm"; \
	fi
	@./bin/blm version
test:
	go vet ./... && go test ./...
# make release                       → next patch version automatically (last remote tag +1)
# make release RELEASE_VERSION=v2.1.0 → explicit
release:
	scripts/release.sh $(RELEASE_VERSION)
.PHONY: build install test release
