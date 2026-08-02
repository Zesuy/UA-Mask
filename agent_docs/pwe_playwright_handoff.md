# Playwright LuCI E2E Handoff — 2026-08-02

## Purpose

This handoff pauses `PWE-010` after subagent execution was interrupted. The implementation is substantial but not complete. Do not discard, reset, stash, clean, commit, or claim completion before reviewing the exact state below.

## Repository state

- Repository: `/home/zesuy/work/github/uamask/UA-Mask`
- Branch: `feature/luci-js-ui`
- HEAD: `f2ba74a feat(luci): replace legacy Lua UI with native JavaScript`
- Remote state: untouched; nothing was pushed, merged, tagged, released, or opened as a PR.
- `PWE-010`: `IN_PROGRESS`
- No QEMU, Playwright, or task-owned Chromium process was running when this handoff was written.
- `git diff --check` passed at pause time.

Current tracked/untracked state:

```text
 M .codex/config.toml
 M .gitignore
 M AGENTS.md
 M Makefile
 M agent_docs/latest_session_work.md
 M agent_docs/project_progress.md
 M agent_docs/workflow/tasks.md
 M openwrt/README.md
?? .codex/agents/sol-advisor.toml
?? .github/workflows/luci-e2e.yml
?? agent_docs/pwe_playwright_handoff.md
?? agent_docs/workflow/tasks/PWE-010.md
?? openwrt/tests/e2e/
```

The two central handoff/progress files and this handoff are main-owned pause updates. `node_modules`, QEMU caches, and run evidence are ignored.

## Agent interruption facts

- `/root/luna_worker` implemented most of the task, then failed at the service quota boundary. The service reported no Luna credits until `2026-08-09 19:21`.
- `/root/pwe_finish` never ran because `deepseek-v4-pro` was not yet available through the Codex integration.
- `/root/pwe_finish_flash` was started as a fallback, then the turn was interrupted; it was explicitly stopped for this pause.
- No live worker remains. A new session must start a fresh bounded worker invocation rather than waiting on these agent IDs.

## Scope and ownership warning

Before `PWE-010`, `.codex/config.toml` already had a preserved, unexplained user modification. During the interrupted agent sequence it changed again, `.codex/agents/sol-advisor.toml` appeared, and `AGENTS.md` gained a Luna-first/Sol-escalation routing section. These files are outside the `PWE-010` allowlist.

Current `.codex/config.toml` diff changes the default/main model, default subagent model, reasoning effort, and concurrency from three to six. The new `sol-advisor.toml` defines a read-only Sol advisor, and the `AGENTS.md` addition documents routing to it. Do not silently commit or delete any of these changes. Attribute them with the user or explicitly separate them from `PWE-010` before integration.

All other `PWE-010` production changes must stay within the card allowlist.

## Implementation currently present

Tracked candidate files:

```text
.github/workflows/luci-e2e.yml
.gitignore
Makefile
openwrt/README.md
openwrt/tests/e2e/package.json
openwrt/tests/e2e/package-lock.json
openwrt/tests/e2e/playwright.config.mjs
openwrt/tests/e2e/fixtures.mjs
openwrt/tests/e2e/scripts/prepare-base.sh
openwrt/tests/e2e/scripts/run.sh
openwrt/tests/e2e/scripts/guest-action.sh
openwrt/tests/e2e/scripts/verify-ipk.sh
openwrt/tests/e2e/tests/smoke.spec.mjs
openwrt/tests/e2e/tests/product.spec.mjs
openwrt/tests/e2e/tests/failure-probe.spec.mjs
```

What is implemented:

- `@playwright/test@1.62.1` and `@axe-core/playwright@4.12.1` are pinned with a lockfile.
- Make targets exist for install, lint, base preparation, smoke, full, package, and intentional failure probe.
- The official OpenWrt 23.05.5 x86/64 combined image is SHA256-verified.
- A fingerprinted ignored LuCI base cache and per-run disposable qcow2 overlay are implemented.
- Test runs use `restrict=on` QEMU user networking with loopback-only SSH/HTTP forwards.
- Fast mode builds/copies current source assets without installing an IPK.
- Package mode accepts an explicit IPK, verifies contents and source view hashes, installs it, then rechecks guest view hashes.
- The runner uses an isolated password, Playwright BrowserContext, bounded readiness loops, per-run artifacts, exact-overlay PID ownership checks, and cleanup traps.
- Smoke and product specs cover routes, desktop/phone/tablet/landscape, console/page/resource failures, overflow, literal null, axe, service states, polling, legacy lists, Save/Apply behavior, invalid candidates, keyboard/focus, diagnostics refresh/clear/missing artifacts, dark mode, and reduced motion.
- A separate GitHub workflow exists and keeps QEMU/Playwright out of core CI.

Ignored local state:

- `openwrt/tests/e2e/node_modules/`: about 22 MiB.
- `.cache/luci-e2e/`: about 250 MiB, including verified raw image, SSH test key, and two prepared base fingerprints.
- `dev/luci-e2e/`: about 180 MiB of run evidence.
- Earlier feasibility runtime: `dev/playwright-luci/`.

## Validation evidence

Confirmed:

- Playwright feasibility using system Chromium passed before implementation.
- Multiple disposable QEMU runs booted, deployed current source, logged into LuCI, rendered all three routes, and cleaned up their owned QEMU/Chromium processes.
- Latest clearly passing smoke run:
  - `dev/luci-e2e/20260802T151208Z-94585-fast-smoke`
  - `result.json`: `playwright_exit=0`
  - `cleanup.txt`: no owned QEMU or Chromium remained.
- Failure runs retained Playwright screenshot, trace, HTML report, telemetry, guest identity, fixture log, and serial log.
- At pause time, host process inspection found no test-owned QEMU, Playwright, or Chromium process.

Not yet accepted:

- No full product suite has a final `playwright_exit=0` result.
- Latest full run:
  - `dev/luci-e2e/20260802T152422Z-16616-fast-full`
  - `playwright_exit=1`
  - cleanup passed.
- Exact-package mode has no completed evidence directory.
- A clean alternate-cache fresh bootstrap has not been independently rerun as a final gate.
- Two consecutive green full runs have not occurred.
- The intentional failure-probe entrypoint has not been independently accepted in its final form.
- Final core/race/OpenWrt contract commands have not been rerun after the current PWE changes.
- The GitHub Actions workflow has not run remotely and must be reviewed statically for cache/security/host assumptions.

## Current failing test

The latest full suite stops at:

```text
product.spec.mjs
blank list add controls save while malformed port and IP are actionable
```

Evidence:

```text
dev/luci-e2e/20260802T152422Z-16616-fast-full/
  test-results/product-UA-Mask-product-be-372a5--port-and-IP-are-actionable/
    error-context.md
    test-failed-1.png
    trace.zip
```

The test fills `70000` into the DynamicList add-control and expects the input itself to gain class `cbi-input-invalid`. On LuCI 23.05 the input remains `cbi-input-text`; native validation may attach state/message to a wrapper or another generated element. This is currently a test assertion problem, not evidence of a production regression.

Next worker should inspect the trace/DOM and assert the user-visible, field-local validation contract using native semantics/message. Do not modify production Settings code merely to satisfy a locator. Repeat the malformed IPv4/CIDR check with the same semantic approach.

Earlier full runs exposed starting/rejected fixture timing and polling-detach assertions. Later edits appear to have progressed beyond those points, but they are not accepted until the full suite runs green from start to finish.

## Items requiring review before completion

1. Fix the DynamicList invalid-value assertion without coupling to a generated class.
2. Run full source mode to the end; fix only genuine test/harness defects within the card allowlist.
3. Run full source mode twice consecutively and record both green run directories.
4. Run `make luci-e2e-failure-probe`; confirm the expected browser failure returns overall success, retains trace/screenshot, and cleanup passes.
5. Run a fresh bootstrap with a new explicit `E2E_CACHE_ROOT` instead of deleting the existing cache, then rerun warm-cache smoke.
6. Build or reuse the reviewed current x86_64 IPK and run exact-package full mode. Confirm `package-identity.json`, package verification, guest installed status, and view hashes.
7. Review the provision script's serial-console pipeline and all timeout/error paths. It has bounded outer `timeout`, but inner wait loops deserve explicit confirmation that no background pipeline can survive interruption.
8. Review GitHub cache contents. The workflow currently caches `.cache/luci-e2e/ssh`; a disposable key can be regenerated and does not need cross-run caching. Avoid making an unnecessary private key available through a restorable CI cache.
9. Confirm failure artifacts contain no password, cookie, storage state, or unrestricted sensitive logs/configuration.
10. Run all task-card final validation commands and main-controller diff review.
11. Resolve or explicitly preserve the out-of-scope `.codex` changes separately.

## Safe resume sequence

```sh
cd /home/zesuy/work/github/uamask/UA-Mask
git switch feature/luci-js-ui
git status --short --branch

sed -n '1,260p' AGENTS.md
sed -n '1,320p' agent_docs/latest_session_work.md
sed -n '1,360p' agent_docs/pwe_playwright_handoff.md
sed -n '1,360p' agent_docs/workflow/tasks/PWE-010.md
```

Then invoke one bounded worker with only `PWE-010`, the current diff, and this handoff. The worker must not update central files or touch `.codex`.

Suggested first validation after inspecting the latest trace:

```sh
make luci-e2e-lint
CHROMIUM_PATH=/snap/bin/chromium make luci-e2e
```

Do not run package/fresh/full repetitions until the current semantic assertion is fixed and one full source run is green.

## Completion gate

`PWE-010` may move to `DONE` only after main independently reviews the complete diff and confirms:

- fresh and warm environment paths;
- two consecutive green full source runs;
- green exact-package run;
- accepted intentional failure evidence and cleanup;
- no task-owned process leak or credential leak;
- CI/static security review;
- all existing core/race/OpenWrt contracts green;
- only authorized files integrated;
- complete phase progress, decision, and handoff updates.

## Resume attempt update — 2026-08-02

The test-only DynamicList correction is present in `openwrt/tests/e2e/tests/product.spec.mjs`: invalid port/IP values are entered with real key events and checked through LuCI's native `data-tooltip` validation message. Lint passed, and full source runs progressed through the DynamicList, polling, keyboard, and smoke scenarios.

The latest full source evidence is `dev/luci-e2e/20260802T162049Z-38367-fast-full/`. It fails at the Diagnostics refresh/clear scenario because both controls are emitted as `<button disabled="false">` and remain disabled. This is a real production defect in `openwrt/luci/htdocs/luci-static/resources/view/uamask/diagnostics.js` (`button()` passes a false boolean into the native LuCI element helper); HTML boolean attributes are disabled by presence, regardless of the string value. The test waits for enabled controls and deliberately does not remove the attribute in the browser.

The production file is forbidden by PWE-010's allowlist, so no production change was made. QEMU/Chromium cleanup passed. A main-owned follow-up/integration task must repair the button attribute semantics before PWE-010 can continue to green full source and complete its package, fresh-cache, repetition, failure-probe, and compatibility gates.

## Final completion update — 2026-08-03

The user authorized the main-owned Diagnostics repair. `button()` now emits `disabled` only when disabled, and the production fix is covered by source and exact-package QEMU runs. PWE-010 is `DONE`; the earlier blocker and pending-gate notes above are historical.

Accepted evidence:

- Source full: `dev/luci-e2e/20260802T163402Z-68587-fast-full` and consecutive repeat `dev/luci-e2e/20260802T163538Z-70741-fast-full`, both 25/25 with cleanup pass.
- Failure probe: `dev/luci-e2e/20260802T163708Z-73379-fast-failure-probe`, expected failure with trace and screenshot, cleanup pass.
- Fresh bootstrap and warm smoke: `/tmp/uamask-e2e-cache.R9zpik` and `dev/luci-e2e/20260802T163834Z-76441-fast-smoke`; keyless-cache fallback `/tmp/uamask-e2e-cache-keyless.EX1T0A` rebuilt the base and generated a fresh key.
- Exact package: `dev/luci-e2e/20260802T164016Z-80808-package-full`, IPK `UAmask 0.4.3-1`, SHA256 `cb919cf40a8a7a072213065da302d417dd877e4d89e9512ab7f2a10bf0256c0b`, guest install and all three installed view hashes verified.
- Final warm smoke after CI/cache changes: `dev/luci-e2e/20260802T164537Z-7945-fast-smoke`.
- `make -C core ci`, race tests, LuCI JS/package contracts, shell syntax, YAML parse, diff check, and artifact/process security review passed.

The CI workflow no longer caches `.cache/luci-e2e/ssh`; `prepare-base.sh` rebuilds a cached base when its disposable private key is absent. No commit or push was performed. The out-of-scope `.codex/*` and `AGENTS.md` changes remain preserved for separate ownership review.
