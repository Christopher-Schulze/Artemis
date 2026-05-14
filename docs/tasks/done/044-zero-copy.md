# TASK 044: Zero-copy + sync.Pool builder for hot paths

## Why

Profile (TASK 040) showed the parser allocates 5221 buffers for medium HTML. Pool the outer `strings.Builder` so the grown buffer survives across calls. Diminishing-returns optimisation given cgo dominates total CPU; still worth landing the infrastructure.

## Done

- [x] `internal/pool` package with `GetBuilder`/`PutBuilder` over `sync.Pool`
- [x] cap retained capacity at 1MB to prevent monstrous-buffer retention
- [x] `agent.HTML` uses pool + `strings.Clone` for safe handoff
- [x] `agent.Text` uses pool + `strings.Clone`

## Numbers

| Bench | Before | After |
|---|---|---|
| Text 50 sections | 12355 ns/op | 12620 ns/op (within noise) |
| Markdown 50 sections | 46128 ns/op | 50120 ns/op (within noise) |

Honest read: the pool addition does not move the needle on the tested workloads because each operation has hundreds of internal allocations beyond the top-level Builder. The pool helps under sustained heavy load (100s of pages/sec) where the outer Builder's grown capacity is repeatedly handed out instead of starting fresh.

## Notes

`agent.Markdown` still allocates a fresh `mdConverter{Builder}` value-type each call. To pool it would require pointer semantics on the Builder field plus careful reset; deferred until profile shows it pays off. Other hot paths (selector matching, text walking) are inside cascadia and x/net/html and out of scope for in-tree optimisation.

Future arena work that *would* matter: per-Page node-slab pool so the parser writes into a pre-allocated arena; that's TASK 041b when we have a workload showing it pays off.
