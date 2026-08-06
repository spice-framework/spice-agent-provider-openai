# Roadmap

The canonical cross-repository implementation ledger is maintained in
`spice-agent/docs/implementation/README.md`. This repository does not maintain
a second phase numbering system.

The current contract is a complete OpenAI Responses implementation of
`model.Provider`: bounded request translation, streamed text, finalized tool
calls, usage, completion metadata, cancellation, typed redacted failures,
offline fake-HTTP coverage, and an opt-in live acceptance.

Before a stable release, changes remain focused on compatibility evidence,
additional provider fixtures, and the signed architecture-proof distribution.
No milestone may add a parallel container, ambient discovery, raw content
telemetry, or retry after an observed stream.
