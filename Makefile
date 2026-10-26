GO ?= go
WEREWOLF ?=
PACKAGE ?=
ARCH ?= $(shell uname -m | sed 's/arm64/aarch64/;s/amd64/x86_64/')
MELANGE ?= $(firstword $(wildcard $(CURDIR)/build/tools/melange) melange)
export MELANGE
.DEFAULT_GOAL := metadata

ports := $(CURDIR)/build/tools/package
sources := $(shell find cmd/package internal -type f \( -name '*.go' -o -name '*.tmpl' \)) go.mod go.sum
select = --package '$(PACKAGE)' --arch '$(ARCH)' $(if $(WOLFI),--wolfi '$(abspath $(WOLFI))')

$(ports): $(sources)
	mkdir -p $(dir $@)
	@test -e build/go.mod || printf 'module werewolf.generated\n\ngo 1.27.0\n' > build/go.mod
	$(GO) build -o $@ ./cmd/package

.PHONY: tools metadata build check test lint updates refresh discover verify derive prepare

tools: $(ports)

metadata: $(ports)
	$(ports) catalog validate

build: metadata
	$(ports) build $(select) $(if $(WEREWOLF),--werewolf '$(abspath $(WEREWOLF))')

check updates refresh discover verify derive render: metadata
	$(ports) $@ $(select)

prepare: metadata
	$(ports) prepare $(select) --fresh

test:
	$(GO) test ./...

lint:
	$(GO) run ./cmd/package schema --dest docs/package.schema.json --offline
	$(GO) vet ./...
	@git ls-files -z --cached --others --exclude-standard --deduplicate '*.go' | xargs -0 -I{} sh -c 'test ! -f "$$1" || gofmt -l "$$1"' sh '{}' | awk '{ print "Run gofmt on: " $$0; bad=1 } END { exit bad }'
	@git ls-files -z --cached --others --exclude-standard --deduplicate '*.go' | xargs -0 -I{} sh -c 'test ! -f "$$1" || awk '"'"'FNR > 2000 { print FILENAME ": exceeds 2000 lines"; exit 1 }'"'"' "$$1"' sh '{}'
