status: PHASE_8_DONE
current_phase: phase_8_playwright_luci_e2e
branch: feature/luci-js-ui
next_executor: main_review
paused_by_user: false

# Project Progress

## Current result

The Lua-to-native-LuCI-JavaScript migration (`LUI-000` through `LUI-070`) remains complete at local commit `f2ba74a`. A new Phase 8 task, `PWE-010`, is adding unattended Playwright/QEMU product checks so normal UI iteration does not require manual package installation or hand-driven browser verification.

## PWE-010 progress

- Pinned Playwright and axe dependencies, an official-image QEMU base preparer, disposable overlay runner, fast source mode, exact-package mode, guest fixtures, smoke/product/failure specs, Make targets, documentation, and a separate CI workflow are present in the uncommitted worktree.
- A complete fast smoke run passed and cleaned up all owned processes.
- Full source mode passed twice consecutively after semantic DynamicList assertions, bounded polling jitter, diagnostics action enablement, and accessible status locators were corrected.
- Exact-package, clean alternate-cache bootstrap, intentional-failure evidence, CI security review, and all final compatibility commands passed.
- No physical router, host firewall, remote branch, tag, release, or host runtime artifact was touched.

## Completed state

`PWE-010` is `DONE` after main review. No worker or task-owned VM/browser process remains active. No physical router, host firewall, remote branch, tag, release, or GitHub state was touched.

## Authoritative handoff

Read `agent_docs/pwe_playwright_handoff.md` for the exact worktree inventory, evidence directories, agent failures, current test defect, out-of-scope `.codex` changes, security considerations, and safe resume sequence.

## Final evidence

- Source full: `dev/luci-e2e/20260802T163402Z-68587-fast-full` — 25/25, cleanup passed.
- Source full repeat: `dev/luci-e2e/20260802T163538Z-70741-fast-full` — 25/25, cleanup passed.
- Intentional failure probe: `dev/luci-e2e/20260802T163708Z-73379-fast-failure-probe` — expected test failure, trace/screenshot retained, cleanup passed.
- Fresh cache bootstrap/warm smoke: `/tmp/uamask-e2e-cache.R9zpik`, `dev/luci-e2e/20260802T163834Z-76441-fast-smoke` — 7/7, cleanup passed.
- Exact package full: `dev/luci-e2e/20260802T164016Z-80808-package-full` — 25/25; IPK `UAmask 0.4.3-1` installed and all three view hashes matched.
- Keyless base-cache fallback: `/tmp/uamask-e2e-cache-keyless.EX1T0A` rebuilt the base and generated a disposable key.
- Final warm smoke after CI/cache changes: `dev/luci-e2e/20260802T164537Z-7945-fast-smoke` — 7/7, cleanup passed.
- Final core/race/OpenWrt contract commands and `git diff --check` passed.

## Resolved resume blocker — 2026-08-02

The DynamicList assertion is semantic and `make luci-e2e-lint` passes. Main authorization allowed the separate production repair in `diagnostics.js`, after which the full source and package gates passed. The original `disabled="false"` defect is closed under decision D-021.
