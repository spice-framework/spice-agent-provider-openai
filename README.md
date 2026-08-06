# Spice Agent OpenAI Provider

`spice-agent-provider-openai` is the explicit OpenAI Responses integration for
Spice Agent. It is instance-owned, caller-context-driven, and designed for
generated Spice dependency injection without a registry or service locator.

Install the module and the exact Spice compiler tool:

```text
go get github.com/spice-framework/spice-agent-provider-openai@<version>
go get -tool github.com/spice-framework/toolchain/cmd/spice@v0.1.0-preview.1
```

Applications opt into the default only with a blank import:

```go
import _ "github.com/spice-framework/spice-agent-provider-openai/autoconfigure"
```

The application supplies `openai.Config` through ordinary typed Spice
configuration. Importing the root package alone never activates a provider.
Phase 0 exposes the reviewed concrete Responses client. The exact
`model.Provider` binding is added only after the core contract is published;
there is no temporary local interface or runtime adapter registry.

See [the dependency review](docs/dependency-review.md),
[security review](docs/security-review.md), and [support matrix](docs/support.md).
