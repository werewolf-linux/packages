# Generate upstream's declared locale fixtures without running its tests.
.PHONY: neutered-locale-fixtures
neutered-locale-fixtures: $(gen-locales)
