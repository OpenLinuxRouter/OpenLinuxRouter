# The MCP surface

Status: **the rules below are enforced by `internal/daemon/conformance_test.go` and
`internal/mcp/mcp_test.go`.** A route or tool that breaks one fails the build.
Section references are to `design.md` unless prefixed `cli:` or `dns:`.

`design.md` §1 calls this project agent-manageable and §6.4 asks for "MCP tools
generated from the same schema". This document says what got built, what did
not, and which test holds each rule.

---

## 1. Where it is

    POST /api/mcp        one JSON-RPC request per call
    GET  /api/mcp        405 — there is no server-to-client stream

Inside `/api` on purpose. Both listeners already authenticate that prefix — the
bearer token on TCP, the socket's mode locally (§6.2) — so the MCP endpoint
inherits it rather than growing a scheme of its own. An MCP endpoint mounted
outside `/api` would be an unauthenticated admin surface on a router.

There is no `olr mcp` subcommand and no second binary. The server is an API
client that happens to live in the same process, which is the next section.

Pointing a client at it:

    claude mcp add --transport http olr http://<router>:8080/api/mcp \
      --header "Authorization: Bearer $(ssh router cat /etc/open-linux-router/api-token)"

## 2. R1 — the MCP package reaches modules only through the API

> **`internal/mcp` imports no module package. Every tool call is an HTTP request
> handed to olrd's own API handler.**

This is the rule the whole surface rests on. A tool call crosses the same
validation, the same global apply lock (§3.6) and the same event publication
that the WebUI and the CLI cross, because there is no other path from this
package to a module. `internal/webui` states the same invariant for the SPA, for
the same reason: §1 says all the surfaces are equal clients of the API, and that
stops being true the first time one of them can reach further than the others.

The import graph enforces it. Nothing else would: a direct import would compile,
the tools would work, and the difference would surface as a change that never
reached the UI or a lock that covered nothing.

> Held by `TestMCPImportsNoModule`.

## 3. R2 — nothing is a list of routes

> **Tools are derived at startup from `/api/routes` and `/api/schema`. A module
> that adds a route gets a tool without `internal/mcp` being edited.**

Modules declare their surface as data (`core.Route`), `core.Server.Mount` builds
the handler *from* that table, and `/api/routes` publishes it. A module
therefore cannot serve a route it did not declare, which is what makes the
published list trustworthy enough to generate from.

This is §3.2 rule 3 — one declaration, many surfaces — applied to routes rather
than to config fields. The config half was already true; this is the half that
lets a *route* reach an agent without being hand-written twice.

> Held by `TestToolsAreDerivedFromThePublishedRoutes`.

## 4. R3 — a tool's arguments are the module's own schema, verbatim

> **A request body reaches `inputSchema` as the exact bytes `core.Reflect`
> produced. Query parameters become top-level properties; the body is nested
> under `config`.**

Not "equivalent to" — the same bytes. Decoding the projection and re-encoding it
through a second JSON Schema implementation would put a translation layer
between the struct tag and the agent, which is the thing §8 chose one dialect to
avoid, and it would silently drop what that implementation does not model. The
`$defs` that `internal/dhcp`'s pools rely on are a live example.

The body is nested rather than spread across the top level because a route may
have both a body and parameters — every mutating route on `gateway` already
carries `dry_run` and `confirm` — and a config field named `confirm` would
otherwise be the same argument as the query parameter named `confirm`.

Plans take the **relaxed** projection: naming one field must not fail validation
for omitting the others (§10).

> Held by `TestBodySchemaIsThePublishedProjectionVerbatim`,
> `TestPlanTakesTheRelaxedProjection`.

## 5. R4 — one operation is named once

> **A tool's first word is a verb from the shared vocabulary
> (`internal/cli/verbs.go`), and the words match the CLI's spelling.**

    olr dhcp show leases     →  dhcp_show_leases
    olr dns  show queries    →  dns_show_queries
    olr gateway status       →  gateway_status

Same argument as `docs/cli.md`'s: verb drift is invisible in review, and MCP is
the one surface nobody reads by hand, so it is where a "get" or a "fetch" would
appear and never be noticed.

This rule earned its place immediately. The dry-run tools were written as
`dhcp_plan`, and the test refused them: `plan` is not a verb the CLI has, because
the CLI spells a dry run as `--dry-run` on `show`. They are `dhcp_show_plan` now
— one operation, one name, whichever surface you meet it on.

> Held by `TestToolVerbsComeFromTheSharedVocabulary`, `TestToolNamesAreUnique`.

## 6. R5 — every read is published, and no write is

> **A non-mutating route must declare a `Tool`. A mutating route must not.**

Stated in that direction deliberately. "These routes are tools" needs editing
whenever a module gains one, and forgetting is silent — the route works and the
agent simply never learns it exists. "Every read must be published" fails loudly,
and an exception has to be argued for in the test.

The second half is the increment boundary. §6.2 says a mutating route plans
before it writes and refuses a `disruptive` plan without `?confirm=true`, and
only `internal/gateway` implements it; `dhcp`, `dns` and `clients` apply on the
first request. Publishing a write today would hand an agent a tool that can drop
the LAN with nothing between the model and the change but a tool name in an
approval dialog — and the human approving it cannot see from `dhcp_set_config`
that it disconnects them. The `409`-plus-plan is what would show them.

So writes wait for the gate. `TestNoMutatingRouteIsPublishedYet` is deleted in
the commit that lands it.

> Held by `TestEveryReadRouteIsPublishedAsATool`,
> `TestNoMutatingRouteIsPublishedYet`.

## 7. R6 — a refusal reaches the model, addressed

> **A non-2xx response becomes a tool result with `isError: true` carrying the
> message and every addressed problem — never a JSON-RPC error.**

A JSON-RPC error is handled by the client and never reaches the model, so a
rejected config reported that way would leave the agent knowing only that
something went wrong. Reported as a tool result it is text the model reads and
can act on:

    The request was refused (HTTP 422): invalid dhcp configuration
      pools[0]: start 192.168.1.10 is above end 10.0.0.5

Those are the same bytes the WebUI attaches to a form field and the CLI prints
under a failed command — one envelope, rendered once, by
`core.ErrorBody.String`. Writing that renderer is what exposed a defect the CLI
had been shipping: `dhcp` and `dns` were putting the rendered problems in the
envelope's `message` *and* in its `problems[]`, so `olr` printed each one twice.
The message summarises; the problems carry the detail.

A protocol-level failure — an unknown tool, an unknown method — stays a JSON-RPC
error, because there is no tool to have failed and the model cannot fix it.

> Held by `TestRefusalIsAToolErrorNamingTheField`,
> `TestFailureTextDoesNotRepeatEachProblem`, `TestUnknownToolIsAProtocolError`.

## 8. The protocol, and why it is written out

`internal/mcp` implements `initialize`, `ping`, `tools/list`, `tools/call` and
the notifications, over the streamable HTTP transport, stateless. Roughly 300
lines. It advertises **2025-06-18** and negotiates down to 2025-03-26 or
2024-11-05.

The official Go SDK was measured first, not assumed away:

| | go-sdk | written out |
|---|---|---|
| Go floor | **1.25** from v1.5.0 | 1.23, unchanged |
| Direct dependencies | 7 | 6, unchanged |
| Transitive additions | 8 at v1.7.0, 2 at v1.2.0 | 0 |
| Protocol drift | theirs | **ours** |

The floor is what decided it. `design.md` §8 commits to `go 1.23` on the grounds
that nothing needs a newer language version, and Debian 13 — the primary target
— has to be able to build this. Taking the SDK means either freezing on v1.2.0,
which speaks the same 2025-06-18 this does, or moving the floor for every
consumer of the project to get a protocol revision.

And what it supplies is mostly what this server does not serve: sessions,
resumability, SSE fan-out, sampling, prompts, resources. A tools-only server is
one POST handler.

What we bought is a real liability — the spec revises roughly twice a year — and
the mitigation is advertising a revision rather than the newest one. Clients
negotiate down. If this server ever needs elicitation, structured content or
server-initiated notifications, that calculation changes and the SDK should win.

**Stateless, and it costs something.** No `Mcp-Session-Id`, no server-to-client
stream, so a tool list that changed mid-connection could not be announced.
olrd's tool list is fixed at startup, so today there is nothing to announce.

## 9. What is not here

- **No writes.** §6, above.
- **No resources or prompts.** Only `tools` is declared in `initialize`, so a
  client will not ask.
- **No output schemas.** §6.2's open gap: config structs are reflected, read and
  plan shapes are not. Tools return JSON as text, which agents read fine, but it
  is the same gap that keeps `web/src/lib/api-types.ts` hand-written. MCP is now
  the third consumer arguing for closing it.
- **No composite operations.** "Set up a guest network" is a core route when it
  exists (§6.3), not something this package assembles — the same rule that keeps
  it out of the WebUI.
