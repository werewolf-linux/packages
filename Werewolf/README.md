# Maintaining the package collection

`Werewolf/` contains shared infrastructure. Lowercase directories at the repository
root contain packages. Package-specific build instructions, tests and usage notes
stay with their `package.yaml`.

Start with the guide for your task:

- [Install and use the packages](docs/usage.md).
- [Contribute or update a package](docs/contributing.md).
- [Read the package definition and command reference](docs/package.md).
- [Understand application packaging](docs/applications.md) or [Wolfi derivation](docs/wolfi.md).
- [Review publication and recovery](docs/design/publishing.md).

## Infrastructure layout

| Location | Purpose |
| --- | --- |
| `cmd/` | The maintenance CLI and helper used inside build guests. |
| `internal/` | Framework implementation, unit tests and workflow regression tests. |
| `pipelines/` | Shared recipe operations, probes and runtime support files. |
| `runner/` | Host sandbox setup, guest configuration and guest bootstrap keys. |
| `docs/` | User guides, contributor instructions, the format reference and design records. |
| `keys/` | Public package-signing trust root. Private signing keys are generated work. |
| `licenses/` | Imported license notices and source provenance. |
| `package.schema.json` | Editor schema generated from the strict definition loader. |
| `tools.lock.json` | Version and checksum pins for maintenance tools. |
| `.work/` | Ignored binaries, caches, captures, architecture workspaces and logs. |

The root `Makefile` is the command entry point. Run `make tools` and
`make test lint check` from the repository root. The Go module lives here, so direct
framework development uses `go -C Werewolf test ./...` from the root. `make test`
also runs the explicit package-local contributor checks declared in package YAML.
