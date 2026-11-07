# leartech-agent-core

The reusable Go library that drives a Ship Proven agent loop, lifted out of the
shell (`leartech-ba-service/cmd/ship-proven`) so the shell and the new
server-side middle layer (`leartech-console-api`) drive the **same** loop from
the **same** code — keeping their behaviour from drifting.

It sits ABOVE `leartech-go-common` (`agentloop`, `aigateway`), not beside it:
go-common is the primitives (the loop state machine, the gateway wire client);
agent-core is the Ship Proven wiring on top (turn the gateway's hosted tools
into loop tools, broker MCP servers, mint a per-session gateway key, …).

## What's here (minimal first)

This first cut lifts only the parts that are **clean** — they already depended
on nothing but go-common — plus the two utilities two of them need:

| package | what | lifted from |
|---|---|---|
| `gatewaytools` | the gateway's hosted tools (`search`, `fetch`) → `agentloop.Tool`, qualified `gateway__<name>`; mounts them and reports the unavailable ones | `cmd/ship-proven/gatewaytools.go` |
| `filelock` | cross-process exclusive lock on a sibling lockfile | `internal/filelock` |
| `safefile` | write a credential file at 0600 via temp+rename (no world-readable window) | `internal/safefile` |

Each kept its tests. `go test ./...` and `go vet ./...` are clean.

## Deliberately NOT here yet

The loop-cycle and MCP-broker files (`agentcycle`, `agentmcp`, `agentprompt`,
`mcpbroker`, `mcpcache`, `sessionkeymint`, `models`, `pricing`, `usagemeter`)
still reference shell-only concerns — a `logUI`/`debugLog` recorder, a
`promptSwapper`, the kubectl-style `clusters` profile store, terminal
rendering (`term`). Lifting them means a small decoupling (value-in/value-out,
transport + credential + rendering supplied by the caller) so a `console-api`
pod with no `term` and no `clusters` file can drive them. That is the next PR,
done one file at a time so each stays proven.

## Consumers

- `leartech-ba-service/cmd/ship-proven` (the shell) — re-points its copies at
  this library as each file lands, so there is ONE copy.
- `leartech-console-api` (the middle layer) — imports this to run the loop
  server-side for the web console.

See `leartech-sp-web`'s MIDDLE-LAYER-PLAN.md for the whole shape.
