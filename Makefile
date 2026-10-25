GO ?= go
WEREWOLF ?= ../werewolf
PACKAGE ?=
ARCH ?= $(shell uname -m | sed 's/arm64/aarch64/;s/amd64/x86_64/')
MELANGE ?= $(firstword $(wildcard $(CURDIR)/build/tools/melange $(CURDIR)/wolfi/build/tools/melange) melange)
export MELANGE
.DEFAULT_GOAL := metadata

ports := $(CURDIR)/build/tools/package
sources := $(shell find cmd/package internal -type f \( -name '*.go' -o -name '*.tmpl' \)) go.mod go.sum
select = --package '$(PACKAGE)' --arch '$(ARCH)' $(if $(WOLFI),--wolfi '$(abspath $(WOLFI))')

$(ports): $(sources)
	mkdir -p $(dir $@)
	$(GO) build -o $@ ./cmd/package

.PHONY: tools metadata build check test lint updates refresh discover verify derive prepare

tools: $(ports)

metadata: $(ports)
	$(ports) catalog validate

build: metadata
	$(ports) build $(select) --werewolf '$(abspath $(WEREWOLF))'

check updates refresh discover verify derive: metadata
	$(ports) $@ $(select)

prepare: metadata
	$(ports) prepare $(select) --fresh

test:
	$(GO) test ./...

lint:
	@files="$$(git ls-files -z --cached --others --exclude-standard '*.go' | xargs -0 gofmt -l)"; if [ -n "$$files" ]; then printf '%s\n' "Run gofmt on:" "$$files"; exit 1; fi
	$(GO) vet ./...
	@git ls-files -z --cached --others --exclude-standard '*.go' | xargs -0 awk 'FNR > 2000 { print FILENAME ": exceeds 2000 lines"; exit 1 }'
