GO ?= go
WEREWOLF ?=
PACKAGE ?=
CATEGORY ?=
ARCH ?= $(shell uname -m | sed 's/arm64/aarch64/;s/amd64/x86_64/')
MELANGE ?= $(firstword $(wildcard $(CURDIR)/Werewolf/.work/tools/melange) melange)
export MELANGE
.DEFAULT_GOAL := metadata

ports := $(CURDIR)/Werewolf/.work/tools/package
sources := $(shell find Werewolf/cmd/package Werewolf/internal -type f \( -name '*.go' -o -name '*.tmpl' \)) Werewolf/go.mod Werewolf/go.sum
select = --package '$(PACKAGE)' --category '$(CATEGORY)' --arch '$(ARCH)' $(if $(WOLFI),--wolfi '$(abspath $(WOLFI))')

$(ports): $(sources)
	mkdir -p $(dir $@)
	$(GO) -C Werewolf build -o $@ ./cmd/package

.PHONY: tools metadata build check test test-packages lint updates refresh discover verify derive render prepare

tools: $(ports)

metadata: tools
	$(ports) catalog validate

build: metadata
	$(ports) build $(select) $(if $(WEREWOLF),--werewolf '$(abspath $(WEREWOLF))')

check updates refresh discover verify derive render: metadata
	$(ports) $@ $(select)

prepare: metadata
	$(ports) prepare $(select) --fresh

test: tools
	$(GO) -C Werewolf test ./...
	$(ports) checks

test-packages: metadata
	$(ports) test $(select) --build --report Werewolf/.work/tests/$(ARCH).json

lint: tools
	$(ports) schema --dest Werewolf/package.schema.json --offline
	$(GO) -C Werewolf vet ./...
	@git ls-files -z --cached --others --exclude-standard --deduplicate '*.go' | xargs -0 -I{} sh -c 'test ! -f "$$1" || gofmt -l "$$1"' sh '{}' | awk '{ print "Run gofmt on: " $$0; bad=1 } END { exit bad }'
	@git ls-files -z --cached --others --exclude-standard --deduplicate '*.go' | xargs -0 -I{} sh -c 'test ! -f "$$1" || awk '"'"'FNR > 2000 { print FILENAME ": exceeds 2000 lines"; exit 1 }'"'"' "$$1"' sh '{}'
