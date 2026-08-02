# Latest Session Work — Playwright LuCI E2E Paused

## Required recovery order

```text
1. Read AGENTS.md
2. Read agent_docs/latest_session_work.md
3. Read agent_docs/project_progress.md
4. Read agent_docs/pwe_playwright_handoff.md completely
5. Read agent_docs/workflow/tasks/PWE-010.md completely
6. Inspect the existing diff and latest failure evidence before running anything
```

## Current state

- Repository: `/home/zesuy/work/github/uamask/UA-Mask`
- Branch: `feature/luci-js-ui`
- HEAD: `f2ba74a`
- Active task: `PWE-010 — Unattended Playwright LuCI Product Checks`
- Task state: `DONE`, completed after main review
- Remote/physical state: untouched
- Live agents: none
- Live task-owned QEMU/Chromium/Playwright processes: none at pause time
- Do not reset, stash, clean, commit, push, or overwrite the current worktree on resume.

## What exists

- A substantial Playwright/QEMU implementation is present under `openwrt/tests/e2e/`, with Make targets, documentation, and a separate CI workflow.
- Fast source smoke has a green evidence run.
- Full source, exact-package, fresh-cache, repeated-green, failure-probe, and final compatibility gates are green.
- Complete evidence, file inventory, agent failures, current defect, security notes, and resume commands are in `agent_docs/pwe_playwright_handoff.md`.

## Important scope warning

`.codex/config.toml` changed again, `.codex/agents/sol-advisor.toml` appeared, and `AGENTS.md` gained Luna/Sol routing instructions during the interrupted agent sequence, all outside the PWE allowlist. Preserve them, but do not silently integrate or delete them. Resolve their ownership separately from `PWE-010`.

## Historical next action

This was the pre-resume handoff instruction. It has been completed; no additional worker is active or required for PWE-010.

## Resume attempt — 2026-08-02

- The DynamicList test was changed within `openwrt/tests/e2e/**` to type invalid values through the LuCI validator and assert the native `data-tooltip` message rather than a generated CSS class.
- `make luci-e2e-lint` passed.
- Full source runs reached all prior scenarios, including DynamicList, keyboard, polling, and smoke. Each failed run cleaned up its owned QEMU and Chromium processes.
- Latest run: `dev/luci-e2e/20260802T162049Z-38367-fast-full/`; it reaches Diagnostics and fails because both `Refresh logs` and `Clear logs` render as `<button disabled="false">` and remain disabled.
- Root cause is outside the PWE-010 allowlist: `openwrt/luci/htdocs/luci-static/resources/view/uamask/diagnostics.js` passes a false boolean to the native `E()` helper. In HTML, `disabled="false"` is still a disabled boolean attribute.
- The PWE test now waits for the buttons to be enabled, so it exposes the real product defect instead of bypassing it. No production file was changed.
- Main authorization allowed the separate production repair in `diagnostics.js`; the task is now complete.

## Completion — 2026-08-03

- `PWE-010` moved to `DONE` after main independently reviewed the allowlisted E2E changes and the authorized main-owned Diagnostics repair.
- Source full passed twice consecutively: `20260802T163402Z-68587-fast-full` and `20260802T163538Z-70741-fast-full`.
- Intentional failure probe passed its harness gate with retained trace/screenshot: `20260802T163708Z-73379-fast-failure-probe`.
- Fresh-cache bootstrap and warm smoke passed at `/tmp/uamask-e2e-cache.R9zpik`; keyless base-cache fallback was separately exercised at `/tmp/uamask-e2e-cache-keyless.EX1T0A`.
- Exact-package full passed: `20260802T164016Z-80808-package-full`, IPK SHA256 `cb919cf40a8a7a072213065da302d417dd877e4d89e9512ab7f2a10bf0256c0b`, guest package identity and all three installed view hashes verified.
- Final warm smoke after CI/cache changes passed: `20260802T164537Z-7945-fast-smoke`.
- `make -C core ci`, `go test -race -count=1 ./...`, LuCI JS/package contracts, shell syntax, workflow YAML parse, and `git diff --check` passed.
- CI no longer caches the guest private key; failure artifacts contained no password/cookie/storage filenames, and no owned QEMU/Chromium process remains.
- No commit, push, remote branch, release, physical router, or host firewall operation was performed.
