# Decision Log

Only the main controller updates this file.

## 2026-08-02

### D-001 — Native LuCI JavaScript, not line-by-line translation

Status: accepted.

The migration uses native LuCI JavaScript views and product-oriented information architecture. It does not preserve the legacy single-page layout merely for implementation parity.

### D-002 — Compatibility precedes interaction redesign

Status: accepted.

UCI normalization, JSON/CLI compatibility, lifecycle safety, adapter boundaries, menu, ACL, and package wiring must stabilize before page tasks run.

### D-003 — Preserve UCI keys and normalize storage

Status: accepted.

Existing option names remain. Legacy scalar CSV/space-separated forms are accepted; canonical UCI list storage is used after a successful save.

### D-004 — Core remains authoritative

Status: accepted.

The UI may provide inline checks, but `UAmask -check-config` remains the authoritative configuration validator. The UI adapter does not duplicate business policy.

### D-005 — Minimal constrained adapter

Status: accepted in principle; exact API requires main review in `LUI-020`/`LUI-021`.

Runtime status, version, stats, logs, effective configuration, and last error use one narrow adapter. Browser input cannot choose arbitrary paths or commands.

### D-006 — OpenWrt 23.05 is the LuCI API floor

Status: accepted.

JavaScript uses APIs available in OpenWrt 23.05.5 and is packaged for both 23.05.5 IPK and 25.12.5 APK generations.

### D-007 — Page-level parallelism only

Status: accepted.

Shared init/UCI/adapter/menu/ACL/package work is serial. Overview, settings, and diagnostics may run as at most three parallel tasks after shared infrastructure is complete.

### D-008 — Lua removal is a late integration gate

Status: accepted.

Legacy Lua files and `luci-compat` remain until all replacement views and compatibility contracts pass. Only `LUI-060` may remove them.

### D-009 — Project-scoped Luna worker

Status: accepted.

The named `luna_worker` uses `gpt-5.6-luna`, `model_reasoning_effort = "max"`, and `workspace-write`. It cannot spawn subagents and executes one allowlisted task card at a time.

### D-010 — Native theme over generated design system styling

Status: accepted.

Product principles such as progressive disclosure, actionable feedback, accessibility, and mobile layout apply. Generated custom colors, fonts, animation, and visual frameworks do not; native LuCI theming remains authoritative.

### D-011 — Dual UCI encoding with explicit OpenWrt defaults

Status: accepted after Phase 1 review.

The init path detects canonical UCI lists through libubox list metadata and otherwise preserves legacy CSV/whitespace scalar parsing. New installations explicitly contain all 28 reviewed defaults. Existing scalar list-like defaults remain valid, while successful JavaScript saves may normalize them to canonical lists without changing option names or schema-v1 output.

### D-012 — Explicit transactional lifecycle and fixed error artifact

Status: accepted after Phase 1 review.

Save & Apply uses an explicit start, reload, or stop action; no implicit UCI reload trigger is registered. Reload validates and atomically installs the candidate JSON before updating procd or firewall state, so invalid changes leave a healthy process and the last valid JSON untouched. Validation/configuration/start/stop failures are stored at `/var/run/UAmask/last_error` as mode-0600 `stage=<value>` and `message=<value>` lines, and successful recovery clears the artifact.

### D-013 — procd reload updates the reviewed instance definition

Status: accepted after Phase 1 review.

The core has no in-process configuration reload. A valid reload therefore re-registers the same named procd instance through the OpenWrt 23.05 `rc_procd` path using the fixed `UAmask -config /var/run/UAmask/config.json` command and file dependency. This is one lifecycle update and does not explicitly stop the healthy process before candidate validation.

### D-014 — Fixed adapter actions and status semantics

Status: accepted after Phase 2 review.

`/usr/libexec/uamask-luci` exposes only `status`, `stats`, `version`, `logs`, `effective-config`, `last-error`, and confirmed `clear-log`. It rejects browser paths and extra arguments. Starting/running/failed facts come from fixed procd, PID, UCI, and last-error sources; a healthy process with a rejected candidate remains `running` with a `configuration-error` anomaly. Effective configuration is resolved through the authoritative core dump command. Log access is limited to canonical files below `/tmp/UAmask` or `/var/log/UAmask` and rejects symlink escape.

### D-015 — Lowercase routes and exact executable ACL

Status: accepted after Phase 2 review.

The stable routes are `admin/services/uamask/{overview,settings,diagnostics}`. The `luci-app-uamask` ACL grants UCI access only to `UAmask`, execution only of the fixed adapter for reads, and execution only of that adapter plus `/etc/init.d/UAmask` for writes. It grants no generic `luci.setInitAction`, wildcard file, arbitrary command, or arbitrary ubus permission. `luci-base` is added while `luci-compat` and legacy Lua remain until `LUI-060`.

### D-016 — Native pages preserve one explicit apply lifecycle

Status: accepted after page review.

Overview polls the fixed status adapter every five seconds with overlap and detach guards. Settings reads legacy scalar values, writes canonical lists (including an explicit empty-list marker), uses native UCI save/apply, then invokes exactly one `/etc/init.d/UAmask reload`; Save alone performs no lifecycle action. Diagnostics renders adapter content as text and requires both UI confirmation and the adapter's fixed token before log clear.

### D-017 — Product state and risk language are factual

Status: accepted after page review.

The pages distinguish exact full-UA pass-through from case-sensitive substring target offload, describe `all` as detected cleartext HTTP only, and place bypass/disconnect behavior behind explicit leakage and disruption language. A rejected candidate while the prior process remains healthy is shown as running with a configuration error, not as a dead service.

### D-018 — Native replacement passed before Lua removal

Status: accepted in `LUI-060`.

All routes, JavaScript syntax checks, adapter and ACL contracts, configuration/lifecycle tests, package-source assertions, core CI, and diff checks passed while legacy Lua was still present. Only after that green gate were the Lua controller/CBI files, their install rules, and `luci-compat` removed. The package source now installs the three JavaScript views, adapter, menu, ACL, init, UCI, and binary with `luci-base` and `libubox`.

### D-019 — QEMU product gate accepts same-overlay cold restart evidence

Status: accepted after Phase 6 review.

The disposable OpenWrt 23.05.5 x86/64 environment passed routes, legacy scalar canonicalization, exactly-one reload, rejected-candidate preservation, controlled service states, bounded diagnostics, confirmed log clearing, five-second non-overlapping polling, 375px layout, keyboard operation, and native light/dark behavior using an IPK rebuilt from the complete reviewed worktree. The local QEMU/GRUB warm-reset path stopped before its second kernel; a cold restart of the exact same disposable overlay produced a new boot identity and restored the package, canonical UCI, procd service, schema-v1 JSON, firewall state, polling, and all three routes. This is accepted as an environment-specific restart limitation, not a waived product recovery check.

### D-020 — User documentation states verified behavior and explicit risk

Status: accepted after final product review.

The primary README, OpenWrt build guide, and tutorial now describe the native Overview/Settings/Diagnostics interface, IPK/APK generations, legacy UCI normalization, authoritative core validation, single Save & Apply lifecycle action, and constrained diagnostics. Obsolete Lua/single-page guidance and unevidenced throughput, CPU reduction, compatibility, and zero-leakage promises were removed. Documentation explicitly limits rewriting to detected cleartext HTTP User-Agent headers and treats port/address bypass, non-HTTP offload, target-keyword offload, and disconnect-on-match as observable leakage or disruption risks rather than hidden performance features.

### D-021 — Diagnostics actions use native boolean-attribute semantics

Status: accepted after Phase 8 product verification.

Diagnostics action buttons pass `null` when enabled and an empty attribute value only when disabled. This avoids emitting HTML `disabled="false"`, which remains disabled because HTML boolean attributes are controlled by presence. Playwright verifies refresh, cancel, confirmed clear-log, and recovery feedback through enabled controls and accessible status messages.

### D-022 — CI never restores the disposable guest private key

Status: accepted after Phase 8 security review.

The GitHub E2E cache stores only verified browser/OpenWrt inputs and the public-key-bearing base image. If the matching private key is absent, `prepare-base.sh` explicitly rebuilds the fingerprinted base with a newly generated disposable key. Failure artifacts do not include passwords, cookies, storage state, or the private key.
