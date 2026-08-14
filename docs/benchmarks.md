# Provider benchmarks

The root-package benchmarks provide deterministic, offline feedback for the
provider's primary translation costs:

- `BenchmarkTranslateRequest` translates a representative three-message
  conversation and one tool definition into Responses API parameters.
- `BenchmarkTranslateCompletedToolCall` translates a finalized function call,
  usage, and bounded metadata without starting a stream pump.
- `BenchmarkScriptedStreamTextAndToolCall` exercises the complete in-memory
  stream adapter from a created event through text, tool call, and completion.
- `BenchmarkRecvCanceled` measures the already-cancelled receive path without
  constructing a transport or goroutine.

Run the suite through the repository-owned offline gate:

```text
make benchmark
```

The gate selects exactly these four benchmarks, runs five fixed 500-iteration
samples with `-cpu=1`, records allocations, forces `GOWORK=off`, and disables
the proxy and checksum database while using the committed vendor graph.
Missing dependencies therefore fail instead of causing a hidden download.
The fixtures use only pre-decoded Responses events and the repository's
in-memory scripted source. They perform no HTTP requests, read no environment
variables, and need no OpenAI credentials.

Results are provisional diagnostic evidence during the pre-1.0 contract stress
period. Preserve the raw `go test` output, Go version, operating system,
architecture, CPU, and exact source revision when comparing runs. Use multiple
samples and `benchstat`; do not turn a single workstation measurement into a
quality-gate threshold. Stable budgets require representative Linux and Windows
history plus an explicit regression policy.

## Initial provisional baseline

The benchmark-introduction tree was sampled three times on 2026-08-09 with Go
1.26.6 on Windows/amd64 and an AMD Ryzen 9 5900X. The table records the median
sample; it is evidence that the benchmark is executable, not a release budget.
The tree was based on commit `a520cec19b2ee572ea1044c8335e7479d70650f3`;
the sampled `provider.go` SHA-256 was
`71d50cc16ccd2c998acc1ea21223a18929100fa73f24f00026bd123f64a0041e`
and the benchmark source SHA-256 was
`f1eff6ee97babd061cae3ec9110bf5bc5d224e457f3be075c4660883efa50f1f`.

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Translate request | 27,730 | 17,821 | 161 |
| Translate completed tool call | 32,698 | 22,268 | 135 |
| Scripted text/tool-call stream | 151,588 | 108,029 | 407 |
| Receive with cancelled context | 29.34 | 0 | 0 |

The scripted stream result intentionally includes operation-context creation,
the bounded pump goroutine, channel handoff, and cooperative close. Scheduler
variance makes that benchmark particularly unsuitable for a fixed threshold
until several supported-platform histories exist.

Future observations use `make benchmark`; the historical table above predates
the repository-owned command and remains descriptive evidence only.
