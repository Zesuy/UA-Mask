# LuCI JavaScript Migration Plan

## Current objective

Replace the legacy Lua CBI/controller UI with a modern native LuCI JavaScript application, preserving existing configuration and runtime contracts while making configuration, status, and diagnostics understandable and safe.

## Non-goals

- Do not change the core session, rewrite, state, policy, offload, firewall, or event architecture.
- Do not change JSON schema version 1 or CLI flags.
- Do not rename existing UCI options in this migration.
- Do not add new runtime statistics or charting in the migration foundation.
- Do not add React, Vue, Svelte, Tailwind, external fonts, or a custom theme.
- Do not operate a physical router or real firewall without explicit authorization.
- Do not delete the Lua UI before replacement pages and compatibility gates pass.

## Confirmed invariants

- UCI is the OpenWrt user configuration source of truth.
- `/var/run/UAmask/config.json` remains generated and atomically validated.
- Core validation is authoritative over UI validation.
- The OpenWrt adapter may expose status and diagnostics but not make policy decisions.
- Existing installations and legacy scalar-list formats remain supported.
- Shared infrastructure is stabilized before page work is parallelized.

## Phases

### Phase 0 — Baseline and contracts

Inventory every current UCI field, default, normalization rule, lifecycle action, installed asset, and user-visible behavior. Add or lock contract tests before migration changes.

Gate: baseline matrix is reviewed and legacy behavior has executable coverage.

### Phase 1 — Compatibility migration

Normalize legacy scalar CSV/space values and canonical UCI lists, align explicit OpenWrt defaults, and establish transactional init/procd semantics. Preserve JSON and CLI contracts.

Gate: old and new UCI representations generate identical valid JSON; invalid candidates preserve the last valid runtime and do not stop it before validation.

### Phase 2 — JavaScript infrastructure

Add the constrained status/log adapter, menu metadata, ACL metadata, package-install wiring, and empty route contract needed by all pages.

Gate: adapter output and permissions are reviewed; menu/ACL/package contracts pass on the lowest supported LuCI API baseline.

### Phases 3–5 — Product pages

After Phase 2 is stable, overview, settings, and diagnostics may run in parallel because their writable source files are separate.

Gate: each page passes syntax, behavior, failure-state, accessibility, and 375px responsive review.

### Phase 6 — Migration switch and verification

Integrate all pages, switch package installation away from Lua, remove legacy files only after dependencies pass, run core/OpenWrt checks, inspect both supported package generations, and execute the OpenWrt 23.05 QEMU smoke suite.

Gate: all automated and QEMU checks pass with no compatibility fallback outside the confirmed design.

### Phase 7 — Final integration review

Review product behavior, security boundaries, diff scope, documentation, and delivery readiness. Resolve conflicts centrally.

## Technical decisions

1. Use native LuCI `view`, `form`, `poll`, `uci`, `ui`, and appropriate native widgets available in OpenWrt 23.05.
2. Treat OpenWrt 23.05.5 as the JavaScript API compatibility floor; verify packaging on 25.12.5.
3. Use canonical UCI list storage for list-like fields while accepting legacy scalars.
4. Keep existing UCI keys to avoid a separate migration program.
5. Add one minimal adapter for structured status/log/effective-config access. It accepts no arbitrary command or path from the browser.
6. Keep runtime JSON generation in init and validation in the core binary; JavaScript validation improves feedback but is not authoritative.
7. Use native theme classes and semantic state text. Do not hard-code a new color or typography system.
8. Poll existing metrics every five seconds only on the overview page.
9. Derive displayed version from installed runtime/package state.
10. Serialize edits to init, adapter, UCI, and package files; allow the three page views to parallelize only after shared contracts are stable.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Legacy scalar values render as one incorrect list item | Contract both scalar and list inputs before UI work; normalize on read/write |
| Save & Apply stops a healthy service before validation | Add transactional lifecycle contract and validate before replacement or stop |
| JavaScript uses APIs missing in OpenWrt 23.05 | Use the 23.05 official LuCI examples as the API floor and test in 23.05 QEMU |
| ACL becomes overly broad to support dynamic log paths | Adapter reads the configured path internally and exposes only fixed methods |
| UI and init defaults diverge again | Record an explicit default matrix and verify generated effective config |
| Page tasks conflict through shared helpers | Stabilize shared adapter/menu/ACL first; keep page writable sets disjoint |
| Removing `luci-compat` breaks package install | Inspect package contents and dependencies for both IPK and APK before deletion gate |
| Client regex validation disagrees with Go RE2 | Keep core validation authoritative and avoid claiming JavaScript regex equivalence |
| UI silently loses old settings | Round-trip legacy fixture configurations and compare effective JSON |

## Main-controller review points

Main must review and approve:

- The baseline field/default/behavior matrix.
- Canonical UCI-list representation and legacy normalization behavior.
- Reload/start/stop transaction design.
- Status/log adapter API and its command/file boundary.
- menu.d and ACL permissions.
- Package dependency change away from `luci-compat`.
- Page information architecture and user-facing risk language.
- The exact moment legacy Lua files are removed.
- QEMU evidence and final compatibility claim.
