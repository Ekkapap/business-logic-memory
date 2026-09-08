VERSION ?= dev
LD = -X github.com/Ekkapap/business-logic-memory/internal/blm.Version=$(VERSION)

build:      ## bin/blm สำหรับเครื่องนี้
	go build -ldflags "$(LD)" -o bin/blm ./cmd/blm
install:    ## ~/.local/bin/blm แล้วเติม PATH
	mkdir -p $(HOME)/.local/bin && go build -ldflags "$(LD)" -o $(HOME)/.local/bin/blm ./cmd/blm && $(HOME)/.local/bin/blm path
test:
	go vet ./... && go test ./...
release:    ## make release VERSION=v2.0.0
	scripts/release.sh $(VERSION)
.PHONY: build install test release
