# Package maintenance metadata

Each `CATEGORY/NAME/metadata.json` has three records: `update`, `hardening` and
`wolfi`. The directory supplies the context name; `update.source` selects the build
and update engine. Neither name nor engine is maintained in a second field.

## Updates

Every package needs one supported automatic source. `wolfi` uses `recipe` or
`family` to select a captured Wolfi recipe. Other sources are `github`,
`github-tags`, `pypi`, `npm`, `minecraft`, `git`, `content` and `plex`. The upstream
updater validates their options, named compatibility streams and lock handlers.
One source owns version selection; bundled components may have their own sources
under `components`.

Only `*-vuln-demo` contexts may set `update.hold`, with a nonempty explanation.
They retain their real upstream source and are skipped before network access.
Production packages cannot opt out. Plex follows the signed public APT repository;
a changed codec ABI stops preparation until its codec manifests are reviewed.

```json
{
  "update": {"source": "github", "repo": "caddyserver/caddy", "prefix": "v"},
  "hardening": {
    "minimized": false,
    "pledge": false,
    "minimized_reason": "PIE is enabled; no feature-minimization claim.",
    "pledge_reason": "Service confinement is declared in the Werewolf form.",
    "evidence": ["caddy.yaml"]
  },
  "wolfi": {
    "relationship": "replacement",
    "recipes": ["caddy.yaml"],
    "changes": [{
      "what": "Build the executable as PIE while keeping the caddy APK name.",
      "why": "Allow ASLR to relocate executable code.",
      "evidence": ["caddy.yaml"]
    }],
    "removal_condition": "Wolfi ships equivalent PIE packaging."
  }
}
```

## Hardening claims

`minimized: true` means the recipe deliberately omits identified unused features,
components or entrypoints, with an explanation and checked-in evidence. It does
not claim every possible reduction has been made. Compiler hardening alone does
not establish minimization.

`pledge: true` means the package itself implements a default-deny capability policy
with explicit grants, as the hardened interpreters do. This is pledge-style
runtime enforcement, not a claim that an OpenBSD pledge syscall is available.
A service launched under Werewolf's `pledge` restrictions does not establish this
package claim; its form remains the authority for service confinement.

Both fields must be JSON booleans, with reasons even when false. False means no
claim has been established, rather than that every build is known to lack the
property. Evidence paths must name existing files inside the context. Installed
package probes remain responsible for proving that protections work.

## Divergence from Wolfi

`derived` means capture Wolfi's recipe and apply checked changes. `replacement`
means an independently maintained recipe replaces identified Wolfi packaging.
`independent` means no Wolfi recipe is tracked as its baseline; it does not assert
that Wolfi has no package for the software.

Record baseline recipe names or family patterns, each meaningful change, why it
exists, evidence, and the condition under which it could be removed. Explain
feature omissions as compatibility differences. Retaining library ABI and APK
names does not promise every feature or default from Wolfi remains available.

## Validation and build identities

`make metadata` validates every context, including both build engines. `make test`
adds update-policy and metadata regression tests. CI uses these same commands.
Package recipes or derivation hooks without metadata fail discovery rather than
silently disappearing from automation.

Metadata is descriptive: changes to its text do not rebuild APKs. Source promotion
still hashes it as reviewed source. Recipe changes, patches and the shared builder
code affect build identities and need an APK epoch or Wolfi policy revision bump.
