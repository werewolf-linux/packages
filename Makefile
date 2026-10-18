# Build through Werewolf so native Linux and macOS use the same runners.
WEREWOLF ?= ../werewolf
PACKAGE ?= solr
ARCH ?= $(shell uname -m | sed 's/arm64/aarch64/;s/amd64/x86_64/')
RECIPES = $(wildcard $(PACKAGE)/*.yaml)
SOURCE ?= ../packages

.PHONY: build check test
build:
	@test -n "$(RECIPES)" || { echo "no recipes for $(PACKAGE)" >&2; exit 1; }
	$(MAKE) -C $(WEREWOLF) howl
	@for recipe in $(RECIPES); do \
		(cd $(WEREWOLF) && build/host/howl build-apk "$(SOURCE)/$$recipe" --arch $(ARCH)) || exit; \
	done

check:
	@for recipe in $(if $(filter command line,$(origin PACKAGE)),$(RECIPES),$(wildcard */*.yaml)); do \
		melange query "$$recipe" '{{.Package.Name}}-{{.Package.Version}}-r{{.Package.Epoch}}{{println}}' || exit; \
	done

test:
	python3 -m unittest discover -s tests -p 'test_*.py'
