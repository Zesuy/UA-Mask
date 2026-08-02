# Verification and Quality Gates

All commands run from the repository root unless a command changes directory explicitly. Real timeouts are hang guards only; business-time tests follow `core/AGENTS.md`.

## Core verification

```sh
make -C core ci
```

This includes formatting, vet, unit/integration tests, build, init shell syntax, configuration generation, and procd contract checks.

## Go race verification

```sh
cd core && go test -race -count=1 ./...
```

Run after all implementation is integrated and whenever a task changes Go code. The current migration is expected not to change Go business code.

## JavaScript syntax

Each view must parse as JavaScript:

```sh
node --check openwrt/luci/htdocs/luci-static/resources/view/uamask/overview.js
node --check openwrt/luci/htdocs/luci-static/resources/view/uamask/settings.js
node --check openwrt/luci/htdocs/luci-static/resources/view/uamask/diagnostics.js
```

The planned aggregate contract is:

```sh
sh openwrt/tests/luci-js-contract-test.sh
```

It must also reject installation references to legacy Lua after the removal gate.

## Menu and ACL checks

```sh
jq -e 'type == "object"' openwrt/luci/root/usr/share/luci/menu.d/luci-app-uamask.json
jq -e 'type == "object"' openwrt/luci/root/usr/share/rpcd/acl.d/luci-app-uamask.json
```

Review requirements:

- Every menu route resolves to an installed view.
- The parent route requires the UA-Mask ACL.
- UCI access is limited to `UAmask`.
- File and command privileges name only the adapter and exact UA-Mask artifacts/actions.
- No wildcard grants arbitrary command execution or arbitrary filesystem access.

## UCI list compatibility

```sh
sh openwrt/tests/config-generation-test.sh
```

Coverage must include:

- Existing scalar CSV keywords and whitelists.
- Existing scalar space-separated devices, ports, and IPv4/CIDR entries.
- Canonical UCI lists for each list-like field.
- Empty values.
- Whitespace, Unicode, quotes, and backslashes.
- Equal generated JSON and equal `-dump-effective-config` output for equivalent scalar/list inputs.
- A save-round-trip contract that does not lose unknown-but-supported existing values.

## Init and procd lifecycle

```sh
sh -n openwrt/root/etc/init.d/UAmask
sh openwrt/tests/procd-contract-test.sh
```

Required scenarios:

- Disabled configuration does not run the service.
- Enabling starts once.
- Valid changes reload once.
- Disabling stops once.
- Candidate JSON is core-validated before atomic replacement.
- Invalid configuration preserves the previous generated JSON.
- Invalid configuration does not stop a healthy process before validation.
- Validation and start failures are persisted for diagnostics and cleared after recovery.

## Adapter verification

The adapter task must add a focused contract test, invoked directly and from `core/Makefile` shell checks. It must cover:

- Structured disabled/running/failed status.
- Version from the installed binary or package source.
- Firewall backend.
- Stats parsing from the fixed stats artifact.
- Last 200 log lines from the configured path without constructing a command string from browser input.
- Effective configuration and last error access.
- Confirmed clear-log behavior.
- Rejection of unknown actions and browser-supplied paths/commands.

## Package content

The planned local contract is:

```sh
sh openwrt/tests/package-assets-test.sh
```

After package creation, inspect IPK/APK contents and dependencies:

- JavaScript views exist at `/www/luci-static/resources/view/uamask/`.
- menu.d and ACL files exist under `/usr/share`.
- The adapter is executable at its fixed installed path.
- Legacy controller/CBI Lua files are absent only after `LUI-060`.
- `luci-base` is required.
- `luci-compat` is absent after `LUI-060`.
- Binary, init script, and `/etc/config/UAmask` remain installed.

Use the existing pinned SDK matrix and `scripts/build-release.sh`; do not download unpinned SDKs or use `--allow-dirty` for deliverable evidence.

## QEMU 23.05 smoke

Use the existing ignored QEMU assets and a disposable overlay. Do not use a physical router.

1. Build an x86_64 OpenWrt 23.05.5 package from the tested commit/worktree state.
2. Boot a disposable OpenWrt 23.05.5 overlay and install the package plus declared dependencies.
3. Confirm `admin/services/uamask/overview`, settings, and diagnostics routes load without Lua compatibility errors.
4. Install or restore a legacy scalar `/etc/config/UAmask` fixture.
5. Confirm settings render each scalar list correctly.
6. Save and apply; verify canonical list persistence and equivalent generated JSON.
7. Verify disabled, starting/running, and failed state presentation using controlled service/config conditions.
8. Confirm overview stats refresh at five-second cadence.
9. Confirm diagnostics show logs, effective configuration, validation error, refresh, and clear confirmation behavior.
10. Reboot the VM and verify procd recovery, UCI preservation, route availability, and valid generated JSON.
11. Capture commands, package identity, screenshots or HTTP evidence, and results in main-owned progress/session files or ignored validation artifacts referenced from them.

## Phase quality gates

| Phase | Gate |
|---|---|
| 0 | Field/default/lifecycle/package inventory reviewed; legacy contracts fail when expected behavior is removed |
| 1 | Scalar and list inputs are equivalent; lifecycle is transactional and errors observable |
| 2 | Adapter API, ACL, menu, and package foundation pass security and compatibility review |
| 3 | Overview states, metrics, risks, polling, accessibility, and 375px layout pass |
| 4 | Settings dependencies, validation, list normalization, plain-language labels, and 375px layout pass |
| 5 | Diagnostics read/refresh/clear/error/effective-config flows pass without path interpolation |
| 6 | Lua removed only after replacements pass; core/race/contracts/packages/QEMU pass |
| 7 | Main confirms invariants, diff scope, security, documentation, and no unresolved blockers |
