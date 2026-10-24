PYTHON ?= python3
WEREWOLF ?= ../werewolf
PACKAGE ?=
ARCH ?= $(shell uname -m | sed 's/arm64/aarch64/;s/amd64/x86_64/')
MELANGE ?= $(firstword $(wildcard $(CURDIR)/build/tools/melange $(CURDIR)/wolfi/build/tools/melange) melange)
export MELANGE
manage = $(PYTHON) scripts/manage.py --package '$(PACKAGE)' --arch '$(ARCH)' --werewolf '$(abspath $(WEREWOLF))' $(if $(WOLFI),--wolfi '$(abspath $(WOLFI))')

.PHONY: build check test updates refresh discover verify derive prepare metadata
metadata:
	$(PYTHON) -c 'import sys; sys.exit("Python 3.12+ is required; set PYTHON to a supported interpreter") if sys.version_info < (3, 12) else None'
	$(PYTHON) scripts/catalog.py validate

build check updates refresh discover verify derive prepare: metadata
	$(manage) $@

test: metadata
	go test ./...
	$(PYTHON) -m unittest discover -s tests -p 'test_*.py'
	$(manage) test-wolfi
