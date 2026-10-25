# Both engines use the root's Go command and package catalog.
PACKAGE ?=
ARCH ?= $(subst arm64,aarch64,$(shell uname -m))
.DEFAULT_GOAL := metadata
.PHONY: metadata test lint refresh discover verify derive prepare build check
metadata test lint refresh discover verify derive prepare build check:
	$(MAKE) -C .. $@ PACKAGE='$(PACKAGE)' ARCH='$(ARCH)' $(if $(WOLFI),WOLFI='$(abspath $(WOLFI))')
