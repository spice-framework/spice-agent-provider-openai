# Architecture

This module is an isolated adapter from OpenAI's Responses API to the public
Spice Agent model contract.

The root package owns validated configuration and an instance-scoped Responses
client. `autoconfigure` is the only library-default activation surface and is
selected solely by an explicit blank import. It contributes a fallback bean so
an application-owned model provider can replace it through ordinary Spice DI.

The adapter may depend on `spice-agent` and `openai-go`; neither dependency may
depend back on this repository. It must not own an agent loop, tool dispatcher,
daemon transport, global client, runtime registry, or package scanner. Requests
remain caller-context-owned, and retries are permitted only before a response
stream begins.

Phase 0 intentionally exposes a concrete client without guessing the unpublished
provider-neutral interface. Once that core interface is tagged, the
autoconfiguration factory will return that exact interface and the compiler will
generate the direct binding.
