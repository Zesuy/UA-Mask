# UA-Mask LuCI JavaScript Product Requirements

## Product intent

UA-Mask will not translate the existing Lua CBI page line by line. It will become a modern, native LuCI JavaScript application while retaining all established core and OpenWrt compatibility contracts.

Implementation order is mandatory: establish compatibility and lifecycle foundations first, then perform interaction redesign.

## Compatibility constraints

- The core production runtime path remains unchanged.
- Existing UCI option names remain compatible.
- Existing `/etc/config/UAmask` installations open, save, and run without manual migration.
- Legacy scalar CSV and space-separated values remain readable. A subsequent save may normalize them into canonical UCI lists.
- JSON schema version 1 remains unchanged.
- The `UAmask -config <path>` CLI contract remains unchanged.
- OpenWrt 23.05.5 and 25.12.5 are supported.
- LuCI native theme behavior, light/dark mode, keyboard access, and mobile use are supported.
- No heavy frontend framework, charting library, Google Fonts, or independent visual system is introduced.

## Current product problems

- All settings are crowded into one Lua CBI page.
- Runtime state is inferred only through `pidof`.
- Save, validation, and start failures lack actionable feedback.
- Keywords, whitelists, devices, ports, and IPs require error-prone handwritten separators.
- Labels such as “modify all traffic” do not match the actual cleartext HTTP behavior.
- Exact User-Agent pass-through and keyword-triggered target offload are easy to confuse.
- Offload behavior does not communicate its bypass and User-Agent leakage risk clearly.
- Performance tuning exposes worker pools, cache sizing, and GOGC too early.
- Defaults differ across Lua, UCI, init, and core.
- Log viewing constructs shell command strings from a user-configurable path.
- The UI version is hard-coded.

## Target information architecture

```text
UA-Mask
├── 概览
│   ├── 运行状态与版本
│   ├── 请求、修改、缓存等指标
│   └── 配置健康与风险提醒
├── 设置
│   ├── 基础设置
│   ├── UA 匹配规则
│   ├── 流量范围
│   ├── 智能卸载
│   └── 高级性能
└── 诊断
    ├── 实际生效配置
    ├── 最近日志
    └── 配置与服务错误
```

## Overview requirements

The overview must:

- Distinguish disabled, starting, running, and failed states.
- Show the actual binary or package version.
- Show the active nftables or iptables backend.
- Show current connections, total requests, modifications, pass-through requests, and cache ratio.
- Show risk reminders when traffic offload or disruptive behavior is enabled.
- Provide a route to settings when the service is not running.
- Refresh existing `/tmp/UAmask.stats` information every five seconds.
- Avoid charting libraries.

## Settings requirements

The settings page must:

- Use `DynamicList` for match keywords, exact pass-through values, and offload keywords.
- Use `DeviceSelect` for ingress devices.
- Use one validated item per bypass port.
- Use one validated IPv4 address or CIDR per bypass entry.
- Give numeric fields ranges, units, and inline error feedback.
- Show the regex input only in regex mode.
- Show expert performance fields only for the custom performance profile.
- Show “disconnect current connection on match” only when User-Agent keyword offload is enabled.
- Read legacy CSV/space-separated scalar values and normalize them to canonical UCI lists on save.
- Clearly distinguish exact User-Agent pass-through from substring keyword target offload.
- Describe “all” mode as applying to detected cleartext HTTP User-Agent headers, not encrypted traffic.
- Place offload and disconnect controls behind explicit consequence and leakage-risk messaging.

## Diagnostics requirements

The diagnostics page must:

- Show the most recent 200 log lines.
- Support explicit refresh.
- Confirm before clearing logs.
- Show the current effective JSON configuration.
- Show the latest configuration-validation or service-start failure.
- Explain anomalous states such as enabled but not running.
- Never interpolate a user-controlled path into a shell command string.
- Surface log-read and action failures with a recovery path.

## Migration scope

### Remove after replacement and gates pass

```text
openwrt/luci/model/cbi/UAmask.lua
openwrt/luci/controller/UAmask.lua
```

Deletion must not occur until compatibility migration, contract coverage, all replacement views, menu/ACL, lifecycle behavior, and package installation checks pass.

### Add

```text
openwrt/luci/htdocs/luci-static/resources/view/uamask/overview.js
openwrt/luci/htdocs/luci-static/resources/view/uamask/settings.js
openwrt/luci/htdocs/luci-static/resources/view/uamask/diagnostics.js
openwrt/luci/root/usr/share/luci/menu.d/luci-app-uamask.json
openwrt/luci/root/usr/share/rpcd/acl.d/luci-app-uamask.json
```

Add one minimal status/log adapter with a narrow, explicit ACL. It is an OpenWrt UI adapter only and must not become a second business runtime path.

### Update

```text
openwrt/package/Makefile
openwrt/root/etc/config/UAmask
openwrt/root/etc/init.d/UAmask
openwrt/tests/
```

- The package installs JavaScript views, menu metadata, ACL metadata, and the adapter.
- `luci-compat` is removed and replaced by the minimal `luci-base` dependency after no Lua compatibility code remains.
- Default UCI explicitly represents new-install defaults.
- Init accepts both scalar legacy values and canonical UCI lists.
- Reload, validation, and error reporting have explicit, tested semantics.

## Save and apply lifecycle

```text
前端字段校验
→ 保存 UCI
→ core -check-config 权威校验
→ 原子生成 config.json
→ 成功后 reload/start/stop
→ 页面轮询确认最终状态
```

Lifecycle requirements:

- Never replace the last valid generated JSON before a candidate passes core validation.
- Invalid configuration must not kill a currently healthy process before validation.
- One Save & Apply operation triggers one explicit lifecycle action.
- The UI displays the authoritative core validation error.
- Disable, enable, reload, and failed-start states are separately testable.

## Security requirements

- ACL access is limited to UA-Mask configuration, status, statistics, logs, effective configuration, and exact required service actions.
- No arbitrary command execution is exposed.
- No arbitrary file access is exposed.
- User-configured paths are never concatenated into shell command strings.
- Clear-log is an explicit write action with confirmation and error feedback.
- Version is read from the installed binary or package metadata, not embedded in JavaScript.

## Product acceptance criteria

- Existing UCI configurations need no manual migration.
- All existing UCI option names remain compatible.
- JSON schema v1 and CLI behavior are unchanged.
- The UI has no horizontal overflow at 375px width.
- A normal user can complete basic configuration without understanding separators, GOGC, cache sizing, or worker pools.
- Dangerous bypass behavior explains its consequences before activation.
- Save, start, log-read, and validation failures provide actionable feedback.
- Native LuCI light/dark themes and keyboard operation work.
- Overview polling is bounded to the existing five-second stats cadence.

## Engineering acceptance criteria

- `make -C core ci` passes.
- `cd core && go test -race -count=1 ./...` passes.
- OpenWrt configuration generation and procd lifecycle contracts pass.
- JavaScript syntax, menu JSON, ACL JSON, package assets, and legacy UCI-list compatibility checks pass.
- OpenWrt 23.05 QEMU verifies page load, legacy configuration read, Save & Apply, service state, and restart recovery.
- OpenWrt 23.05.5 and 25.12.5 package builds contain the correct non-Lua assets.
