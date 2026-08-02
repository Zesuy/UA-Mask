status: COMPLETE
current_phase: phase_7_complete
branch: feature/luci-js-ui
next_executor: user_review

# Project Progress

## Current result

The Lua-to-native-LuCI-JavaScript product migration is complete. `LUI-000` through `LUI-070` are `DONE`; no workflow task remains READY, BLOCKED, or IN_PROGRESS. The branch preserves the existing UCI, schema-v1, CLI, and single-runtime-path contracts while replacing the legacy Lua controller/CBI interface with native Overview, Settings, and Diagnostics pages.

## Delivered product behavior

- Native LuCI JavaScript with native theme behavior, light/dark compatibility, 375px layouts, keyboard operation, bounded loading/error states, and non-overlapping five-second status polling.
- Product-oriented information architecture: Overview for health/metrics/risk, Settings for guided configuration and one explicit Save & Apply lifecycle action, Diagnostics for fixed status/log/effective-config/error actions.
- Legacy CSV/space-separated scalar lists remain readable; successful saves normalize active valid values to canonical UCI lists without changing option names or schema-v1 semantics.
- Invalid candidates preserve the healthy process and last-valid JSON. Recovery and failure facts are actionable and do not silently claim the service stopped.
- Least-privilege UAmask-only UCI/adapter/init ACL; no arbitrary browser-selected path or command.
- OpenWrt 23.05.5 IPK and 25.12.5 APK package generations with `luci-base` and `libubox`, without `luci-compat` or legacy Lua assets.
- User documentation now matches the three-page interface and states cleartext HTTP scope plus bypass/offload/disconnect risks without unsupported performance or zero-leakage claims.

## Verification result

- `make -C core ci` — PASS.
- `cd core && go test -race -count=1 ./...` — PASS.
- `sh openwrt/tests/luci-js-contract-test.sh` — PASS, including literal-null, DynamicList blank-editor, and unchanged legacy-list normalization regressions.
- `sh openwrt/tests/package-assets-test.sh` — PASS.
- Pinned OpenWrt 23.05.5 IPK and 25.12.5 APK package inspection — PASS.
- Disposable OpenWrt 23.05.5 x86/64 QEMU product smoke — PASS for routes, lifecycle, legacy round-trip, failure/recovery, diagnostics, polling, responsive/theme/keyboard behavior, and same-overlay cold restart recovery.
- `git diff --check` — PASS.

## Accepted limitation

The local QEMU/GRUB warm reset stopped before loading its second kernel. Cold-starting the exact same disposable overlay produced a new boot identity and restored package, canonical UCI, procd, schema-v1 JSON, firewall, polling, and all routes. Evidence is retained under ignored `dev/lui062/`; no physical router was operated.

## Remote and release state

No push, merge, pull request, tag, release, physical-device action, or host-firewall change was performed. The authorized single local commit contains the final reviewed migration files and is ready for user review.

## Next action

User reviews the final local branch diff and decides whether to push/open a PR. Any further product behavior change should begin as a new bounded task rather than reopening the completed migration workflow.
