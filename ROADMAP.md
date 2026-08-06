# Roadmap

The canonical cross-repository program is maintained in
`spice-agent/docs/implementation/README.md`.

| Phase | Repository outcome | Status |
|---|---|---|
| 0 | Governance, exact dependency pins, concrete no-network client, manifest, explicit autoconfiguration, and quality gates | in progress |
| 3 | Responses streaming adapter, tool-call translation, usage, cancellation, typed errors, and safe retry behavior | blocked on tagged core model API |
| 6 | Distribution acceptance and signed architecture-proof preview | planned |
| 8 | Stabilized provider authoring and compatibility policy | planned |

No phase may add a parallel container or perform ambient package discovery.
