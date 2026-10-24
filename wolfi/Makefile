PYTHON ?= python3
ARCH ?= $(subst arm64,aarch64,$(shell uname -m))
MELANGE ?= $(if $(wildcard build/tools/melange),$(abspath build/tools/melange),melange)
export MELANGE
# One package: make PACKAGE=php build. refresh and discover take all without it.
PACKAGE ?=
need-package = $(if $(PACKAGE),,$(error name one: make PACKAGE=$(subst $(eval) ,|,$(shell $(PYTHON) ../scripts/catalog.py list --engine wolfi)) $@))

.PHONY: test refresh discover verify derive prepare build check
# Every unit test, shared and each package's; no network, no captured inputs.
test:
	$(PYTHON) -c 'import sys; sys.exit("Python 3.12+ is required; set PYTHON to a supported interpreter") if sys.version_info < (3, 12) else None'
	$(PYTHON) ../scripts/catalog.py validate
	$(PYTHON) -m unittest discover -s tests -p 'test_*.py'
	packages=$$($(PYTHON) ../scripts/catalog.py list --engine wolfi) || exit 1; \
	for package in $$packages; do \
		directory=$$($(PYTHON) ../scripts/catalog.py path $$package --engine wolfi) || exit 1; \
		$(PYTHON) -m unittest discover -s "../$$directory/tests" -p 'test_*.py' || exit 1; \
	done

refresh:
	$(PYTHON) scripts/refresh.py $(if $(WOLFI),--wolfi $(WOLFI)) $(PACKAGE)

# The scheduled check, without reporting to GitHub.
discover:
	$(PYTHON) scripts/discover.py --local $(if $(WOLFI),--wolfi $(WOLFI)) $(PACKAGE)

build/$(PACKAGE)/inputs/upstream.lock.json:
	$(call need-package)
	$(PYTHON) scripts/refresh.py $(if $(WOLFI),--wolfi $(WOLFI)) $(PACKAGE)

verify: build/$(PACKAGE)/inputs/upstream.lock.json
	$(call need-package)
	$(PYTHON) scripts/verify.py $(PACKAGE)

derive: verify
	$(PYTHON) scripts/derive.py $(PACKAGE)

# A fresh checkout each time: prepare never resets an existing one.
prepare: verify
	rm -rf build/$(PACKAGE)/source
	$(PYTHON) scripts/prepare.py $(PACKAGE)

build: derive
	$(PYTHON) scripts/signing-key.py
	$(PYTHON) scripts/build.py $(PACKAGE) --arch $(ARCH)

check: test verify
