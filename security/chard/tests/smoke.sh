#!/bin/sh
set -eu

ulimit -c 0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

version=${1%%_git*}
chard --version | grep -F "chard $version ("
chard --help > help.txt
grep -qF 'measure' help.txt
chard probes --format text > probes.txt
grep -F 'link-defaults' probes.txt

# These ELF checks work on both packaged architectures. Let chard detect the
# execution environment; no kernel ownership assertion is needed for them.
chard measure --cc gcc --probe link-defaults --workdir "$work/probes" -o report.json
jq -e '
  .chard_schema == "1" and
  .coverage.checks_attempted == .coverage.checks_expected and
  .coverage.checks_attempted > 0 and
  .coverage.checks_skipped == 0 and
  ([.observations[] | select(.check == "pie" or .check == "relro")]
    | length == 2 and all(.verdict == "on" or .verdict == "off" or .verdict == "partial"))
' report.json
chard table report.json > table.txt
grep -F 'relro' table.txt
chard digest report.json > digest.json
jq -e --slurpfile report report.json '. == $report[0].digest' digest.json
chard detect relro --evidence report.json > replay.json
chard elf /usr/bin/chard > elf.json
jq -e '.["/usr/bin/chard"] | type == "object"' elf.json
