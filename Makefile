# ไม่ใส่ VERSION = เอา tag ล่าสุดที่ commit นี้สืบทอด (ดึง tag จาก origin ก่อนถ้าถึงได้) เช่น 2.0.6 หรือ 2.0.6-2-g33bc0b6 เมื่ออยู่หลัง tag
VERSION ?= $(shell git fetch -q --tags origin 2>/dev/null; git describe --tags --match 'v*' --always --dirty 2>/dev/null | sed 's/^v//')
LD = -X github.com/Ekkapap/business-logic-memory/internal/blm.Version=$(VERSION)

build:      ## bin/blm สำหรับเครื่องนี้
	go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
install:    ## ~/.local/bin/blm แล้วเติม PATH
	mkdir -p $(HOME)/.local/bin && go build -ldflags "$(LD)" -o $(HOME)/.local/bin/blm ./cmd/blm && $(HOME)/.local/bin/blm path
test:
	go vet ./... && go test ./...
# make release                       → next patch version automatically (last remote tag +1)
# make release RELEASE_VERSION=v2.1.0 → explicit
release:
	scripts/release.sh $(RELEASE_VERSION)
.PHONY: build install test release
