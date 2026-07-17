# AGENTS.md

Guidance for agents working inside `core/`.

## Core Architecture Rules

The core traffic path is layered. Keep these responsibilities separate:

- `ingress` adapts the entry mechanism and emits `model.AcceptedSession`.
- `session` owns the current connection lifecycle and short-lived `SessionState`.
- `state` stores and aggregates observed facts into `TargetProfile` and read-only `ProfileSnapshot`.
- `policy` reads `SessionState` and `ProfileSnapshot`, then emits `PolicyDecision` or `OffloadSignal`.
- `offload` executes explicit offload signals and reports factual success or failure events.

Do not move long-lived target profile state into `session`. `session` may track only current-connection facts such as protocol, consecutive rewrite/no-rewrite counters, and flow offload status.

## Event And Signal Boundaries

`SessionEvent` is a fact: something observed in the current connection or by an executor.

Examples:

- `SessionOpened`
- `ProtocolClassified`
- `HTTPRequestObserved`
- `RewriteApplied`
- `RewriteSkipped`
- `NonHTTPObserved`
- `SessionClosed`
- `FlowOffloadSucceeded`
- `FlowOffloadFailed`

`SessionEvent` must not mean "please do this". Avoid event names or payload fields such as `RequestTargetOffload`, `TryBypass`, or `ShouldPromote`.

`OffloadSignal` is an explicit action candidate produced by policy:

- `TryFlowOffload`
- `TryTargetOffload`
- `CancelTargetOffload`
- `NoAction`

Target-level offload must follow this data flow:

```text
SessionEvent -> StateStore -> ProfileSnapshot -> policy.TargetEvaluator -> OffloadSignal -> offload.Coordinator
```

Flow-level offload may be decided in the synchronous session path, but it must still enter offload through an explicit `OffloadSignal`.

## Synchronous Vs Async Work

The current connection path must remain synchronous and bounded:

```text
AcceptedSession -> SessionState -> classify -> ProfileSnapshot -> policy -> forward/rewrite/drop
```

Async fan-out is for observations and projections:

```text
SessionEvent -> StateStore aggregation
SessionEvent -> stats projection
SessionEvent -> offload observation
```

Do not make the current connection wait for target profile aggregation unless a specific synchronous snapshot read is required for policy evaluation.

## Package Notes

- `internal/model`: shared vocabulary only. Keep behavior out of this package.
- `internal/ingress`: entry adapters. Current implementation is redirect; keep the interface open for future tproxy-style adapters.
- `internal/session`: connection orchestration, protocol classification, forwarding, and event emission.
- `internal/state`: fact aggregation only. It must not decide rewrite or offload actions.
- `internal/policy`: decisions only. It should not mutate profile state or execute offload.
- `internal/offload`: explicit action execution and factual result events.
- `internal/rewrite`: User-Agent rewrite rules only. It must not directly enqueue firewall or offload actions.
- `internal/firewall`: legacy target offload execution primitives. New orchestration should go through `internal/offload`.

## Testing Expectations

Before finishing a core change, run:

```sh
make ci
```

For changes touching architecture boundaries, add or update focused tests:

- `state`: profiles are derived only from factual events.
- `policy`: decisions are derived from session input and profile snapshots.
- `offload`: actions happen only from explicit `OffloadSignal`.
- `session`: connection-local state does not own target profile data.

Use:

```sh
make cover
```

when changing cross-package behavior or preparing a PR that needs coverage output.

## Compatibility

Keep existing CLI flags and user-visible behavior compatible unless the task explicitly asks for a breaking change.

Existing target-level firewall bypass behavior should remain expressible as target offload. Future eBPF flow offload should integrate through `offload.FlowOffloader` without making `session`, `state`, or `rewrite` depend on eBPF implementation details.
