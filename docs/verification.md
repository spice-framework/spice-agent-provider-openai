# Verification

On a fresh clone, explicitly populate the exact product and tools module graphs:

```text
make tools-bootstrap
```

This is the only network-enabled quality mode. It requires Go 1.26.5, downloads
`all` from private temporary copies of both `go.mod`/`go.sum` pairs, disables Go
authentication, and permits only the public checksum database and module proxy.
It verifies that the repository is byte-for-byte unchanged even when a download
fails. A repository without a tools module is valid. No application API keys,
tokens, passwords, or secrets are passed to the Go subprocess.
Every child Go command uses the selected Go 1.26.5 binary from `runtime.GOROOT`,
not an older `go` that may appear first on `PATH`.

- `make fast` checks the exact Go version and runs affected repository tests.
- `make check` adds goimports/gofumpt, tidy diff, vet, and shuffled tests.
- `make benchmark` runs the four adopted deterministic provider benchmarks as
  five fixed 500-iteration, single-CPU samples with the offline vendor graph.
- `make verify` adds allowlisted lint, NilAway, gosec, govulncheck, race tests,
  85% product coverage, reproducible vendor comparison, and vendor-offline
  tests/builds.

The verifier is repository-owned Go and behaves identically from PowerShell,
Linux shells, and macOS shells. It never rewrites product source. `make fmt` is
the only formatting target that mutates Go files. `make fast`, `make check`, and
`make verify` force `GOPROXY=off`; a missing cache entry fails with an actionable
prompt to run the explicit bootstrap.

Every verification mode first validates the exact root module graph and
canonical `spice-compatibility.json`. The release candidate permits no
`replace`, `exclude`, `retract`, `godebug`, or `ignore` directives and requires
the exact declared Go tool. This makes stale Agent or Spice selections,
dependency additions, directness drift, local replacements, and compatibility
metadata drift fail before product tests.

Repository identity also requires `.github/workflows/release.yml` to be the
exact single-job, secret-free caller of the organization Go-module release
workflow at audited commit
`0fcd43dc8b41fad56c231d0e136ad8c762276ed5`. The caller denies workflow-level
permissions and grants only `contents`, `id-token`, `attestations`, and
`artifact-metadata` writes to the reusable job. Extra permissions, local steps,
additional jobs, module or pin drift, and any secret forwarding fail closed.

Clean checkouts preserve repository text as LF on every supported operating
system. CI runs the explicit network-enabled bootstrap before the offline
quality contract, so Windows module-file checks and empty hosted caches exercise
the same deterministic contract as local development.

The live provider proof is deliberately outside this contract. It is compiled
only with the `openai_live` build tag. Canonical compatible-provider mode also
requires `SPICE_RESPONSES_LIVE=1`, `OPENAI_API_KEY`, an explicit
`OPENAI_BASE_URL`, and an explicit `OPENAI_MODEL`; it has no default endpoint
fallback. OpenRouter mode accepts only an exact `:free` model after a bounded
public-catalog zero-price preflight. Optional first-party evidence uses the
mutually exclusive `SPICE_OPENAI_LIVE=1` mode and the exact explicit OpenAI base
URL. Run `make live-acceptance` only when network access and the selected
provider's account terms are authorized; see the README for invocations and the
sanitized evidence boundary.

The provisional offline translation and stream benchmarks are documented in
[`benchmarks.md`](benchmarks.md). They are diagnostic evidence produced by the
repository-owned benchmark gate, not fixed quality-gate thresholds, and use
only the committed vendor graph and in-memory scripted response source.
