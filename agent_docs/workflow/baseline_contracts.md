# UA-Mask LuCI Migration Baseline Contracts

Status: reviewed current-behavior inventory for `LUI-000` at `main@83846d3`.

This document records the behavior that exists before the Lua CBI to native LuCI JavaScript migration. It is evidence for compatibility tests, not a requirement to preserve every usability or safety defect. Product-facing changes remain governed by `product_requirements.md` and the task cards.

## Source-of-truth order

For an installed OpenWrt system, UCI is the editable source. The init script reads UCI, emits a complete schema-v1 JSON candidate, validates it with `UAmask -check-config -config <candidate>`, and atomically replaces `/var/run/UAmask/config.json` only after validation. The core then parses that JSON, applies performance-profile overrides, normalizes values, and validates the effective configuration.

Defaults therefore have four distinct meanings:

1. **Shipped UCI** — values present on a new package installation.
2. **Lua UI** — values or placeholders shown when an option is missing.
3. **Init fallback** — values used when UCI does not provide the option.
4. **Core default/effective** — library defaults and the value after JSON/profile processing.

## Complete UCI field matrix

Every current field appears exactly once below. All fields are currently scalar UCI `option` values; there are no shipped UCI `list` entries.

| Section / option | Current input and encoding | Shipped UCI | Lua UI default or dependency | Init fallback and JSON/runtime destination | Core effective behavior and validation | Classification |
|---|---|---|---|---|---|---|
| `enabled.enabled` | Flag, scalar `0`/`1` | `0` | Flag; no explicit Lua default | `0`; gates `start_service`, not written to JSON | No core field | Compatibility contract |
| `main.operating_profile` | Enum `Low`, `Medium`, `High`, `custom` | absent | `Medium`; custom fields shown only for `custom` | `Medium` → `performance.profile` | Normalized case-insensitively to `low`, `medium`, `high`, `custom`; `high_throughput` is also accepted by core. Raw core default is `custom`; emitted `Medium` becomes `medium` | Intentional OpenWrt override; explicit-default gap |
| `main.buffer_size` | Unsigned integer bytes | absent | `8192`; only with custom profile | `8192` → `performance.buffer_size` | Custom: 1024–65536. Presets override to 8192. Raw core default 8192 | Compatibility contract |
| `main.pool_size` | Unsigned integer | absent | `0`; only with custom profile | `0` → `performance.pool_size` | Custom: ≥0. Presets override to Low 200, Medium 500, High 1000. Raw core default 0 | Compatibility contract |
| `main.cache_size` | Unsigned integer entries | absent | `1000`; only with custom profile | `2000` → `performance.cache_size` | Custom: ≥0. Presets override to Low 2000, Medium 3000, High 5000. Raw core default 1000 | Product defect: three defaults differ |
| `main.gogc_value` | Unsigned integer percent | absent | `100`; only with custom profile | `100` → `performance.gc_percent` | Custom accepts ≥-1, although Lua cannot enter -1. Presets override to 100. Raw core default 100 | Compatibility contract; UI/core range mismatch |
| `main.ua` | Free-form string | `FFF` | `FFF` | `FFF` → `rewrite.user_agent` | No non-empty validation; used as replacement value | Compatibility contract |
| `main.match_mode` | Enum `keywords`, `regex`, `all` | absent | `keywords`; controls keyword/regex fields | `keywords` → `rewrite.mode` | Exact enum after trim/lowercase. `all` sets force-replace; `regex` compiles a case-insensitive Go RE2 expression; otherwise keyword mode | Compatibility contract |
| `main.keywords` | Legacy scalar CSV; surrounding item whitespace trimmed; empty items dropped | absent | `Windows,Linux,Android,iPhone,Macintosh,iPad,OpenHarmony`; shown only in keyword mode | `iPhone,iPad,Android,Macintosh,Windows` → `rewrite.keywords` array | Raw core default is the same five-item set as init. Each match is case-sensitive substring containment | Product defect: Lua and effective missing-value defaults differ |
| `main.ua_regex` | Go RE2 pattern as scalar string | `(iPhone\|iPad\|Android\|Macintosh\|Windows\|Linux\|Apple\|Mac OS X\|Mobile)` | `(iPhone\|iPad\|Android\|Macintosh\|Windows\|Linux)`; shown only in regex mode | Six-item Lua pattern → `rewrite.pattern` | Raw core default is the shipped nine-item pattern. Regex mode requires non-blank input and compiles `(?i)` + pattern; ignored for matching in keyword/all modes | Product defect: three sources differ |
| `main.replace_method` | Enum `full`, `partial` | absent | `full`; shown only in regex mode | `full`; `partial` maps to `rewrite.partial_replace=true` | Partial replacement is used only with a compiled regex; otherwise the whole UA becomes the replacement | Compatibility contract |
| `main.whitelist` | Legacy scalar CSV; trimmed; empty items dropped | empty scalar | No default, empty placeholder | empty → `rewrite.whitelist` array | Case-sensitive exact full User-Agent equality; pass-through is cacheable | Compatibility contract; wording must remain distinct from offload keywords |
| `main.port` | Port integer | `12032` | `12032`; Lua `port` datatype | `12032` → `listen.port` | 1–65535. Raw core default 8080; OpenWrt intentionally overrides it | Intentional platform override |
| `main.iface` | Legacy scalar whitespace-separated device names | `br-lan` | `br-lan`; free-text field | `br-lan`; used only to build ingress firewall rules | No JSON/core field. Shell word splitting creates one rule per interface; nft formatting uses a quoted scalar/set | Compatibility contract; product input defect |
| `main.enable_firewall_set` | Flag, scalar `0`/`1` | absent | Explicit Lua default `0` | `0`; controls set creation and forcibly disables the three offload values below when false | No direct JSON field. The core can still receive only the gated offload values | Compatibility and safety contract |
| `main.Firewall_ua_bypass` | Flag, scalar `0`/`1` | absent | False when unset; shown when set offload is enabled | `0` → `firewall.non_http.enabled`, forced false when set offload is disabled | Enables target offload after threshold, delay, and HTTP-cooldown policy | Compatibility contract |
| `main.Firewall_ua_whitelist` | Legacy scalar CSV; trimmed; empty items dropped | absent | Empty placeholder; shown when set offload is enabled | empty → `firewall.ua_whitelist` array, forced empty when set offload is disabled | Each item is a case-sensitive UA substring. A hit schedules target IP:port offload for 24h and skips rewrite | Compatibility contract; dangerous behavior needs explicit risk copy |
| `main.Firewall_drop_on_match` | Flag, scalar `0`/`1` | absent | False when unset; shown when set offload is enabled | `0` → `firewall.drop_on_match`, forced false when set offload is disabled | On an offload-keyword hit, the current HTTP connection is dropped after the offload fact is produced | Compatibility contract; disruptive control |
| `main.proxy_host` | Flag, scalar `0`/`1` | `0` | False when unset | Effectively false when missing; controls OUTPUT rules and process group | No JSON/core field. When true, creates/uses GID group and proxies router-originated TCP traffic | Compatibility contract |
| `main.bypass_gid` | Unsigned integer GID | `65533` | `65533`; shown only when host proxy is enabled | `65533`; firewall/group setup only | No JSON/core field. OUTPUT also always exempts GID 65534 for OpenClash | Compatibility contract |
| `main.bypass_ports` | Legacy scalar whitespace-separated ports | `22 443` | Placeholder `22 443`, not a default | Empty when missing; firewall rules only | No JSON/core field and no init range validation. iptables joins items for `multiport`; nft creates a port set | Product defect: deleting the shipped option changes behavior despite placeholder |
| `main.bypass_ips` | Legacy scalar whitespace-separated IPv4/CIDR entries | `172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 169.254.0.0/16` | Same string as an actual Lua default | Empty when missing; firewall rules only | No JSON/core field and no init address validation; each shell word becomes a rule/item | Product defect: Lua and init missing-value behavior differ |
| `main.firewall_advanced_settings` | Flag, scalar `0`/`1` | absent | False when unset; controls three tuning fields | `0`; when false, resets threshold/delay/timeout to safe defaults before JSON generation | No JSON/core field | Compatibility and safety contract |
| `main.firewall_nonhttp_threshold` | Unsigned integer event count | absent | `5`; shown with advanced settings | `5` → `firewall.non_http.threshold`; advanced values <1 fall back to 5 | Core requires ≥1 | Compatibility contract |
| `main.firewall_decision_delay` | Unsigned integer seconds | absent | `60`; shown with advanced settings | `60` → `firewall.non_http.decision_delay`; advanced values <10 fall back to 60 | Core allows non-negative duration, so init deliberately has a stricter 10-second safety floor | Intentional platform safety override |
| `main.firewall_timeout` | Unsigned integer seconds | absent | `28800`; shown with advanced settings | `28800` → `firewall.non_http.timeout`; advanced values <60 fall back to 28800 | Core requires positive whole seconds; init deliberately has a stricter 60-second safety floor | Intentional platform safety override |
| `main.log_level` | Enum debug/info/warn/error/fatal/panic | `info` | `info` | `info` → `observe.log_level` | Lowercased and parsed by logrus; invalid values fail validation | Compatibility contract |
| `main.log_file` | Free-form absolute-or-relative path string | `/tmp/UAmask/UAmask.log` | Placeholder only; blank means no core file logging | `/tmp/UAmask/UAmask.log` → `observe.log_file`; parent directory is created before validation | Raw core default is empty. Current Lua interpolates this user-controlled value into shell strings for tail/clear | Compatibility contract for value; security defect in UI access |

## List and empty-value behavior

| Logical list | Current legacy scalar separator | Current normalization | Empty behavior | Current canonical UCI-list support |
|---|---|---|---|---|
| Rewrite keywords | comma | Split on commas, trim surrounding whitespace, drop empty items | JSON `[]` | Not explicitly handled or tested |
| Exact UA pass-through | comma | Split on commas, trim surrounding whitespace, drop empty items | JSON `[]` | Not explicitly handled or tested |
| Offload UA keywords | comma | Split on commas, trim surrounding whitespace, drop empty items | JSON `[]` | Not explicitly handled or tested |
| Ingress devices | shell whitespace | Shell word splitting; no trimming/validation beyond splitting | No ingress device items | Not explicitly handled or tested |
| Bypass ports | shell whitespace | Shell word splitting; iptables later joins with commas | No bypass-port rule | Not explicitly handled or tested |
| Bypass IPv4/CIDR | shell whitespace | Shell word splitting | No bypass-address rule | Not explicitly handled or tested |

CSV matching is case-sensitive after normalization. Empty CSV elements such as `A,, B,` disappear. Space-separated fields accept repeated whitespace because the shell splits them, but values containing whitespace cannot be represented. `LUI-001` must lock these scalar behaviors before `LUI-010` adds canonical UCI-list input.

## Generated JSON fields without editable UCI options

The init script always emits these fixed or detected schema-v1 values:

| JSON field | Init value | Core behavior |
|---|---|---|
| `schema_version` | `1` | Must equal 1 |
| `listen.dial_timeout` | `30s` | Must be positive |
| `listen.client_keep_alive` | `3m` | Must be non-negative |
| `listen.upstream_keep_alive` | `3m` | Must be non-negative |
| `firewall.backend` | detected `nft` when `fw4` exists, otherwise `ipt` | Lowercased; only `nft` or `ipt` accepted |
| `firewall.set_name` | `UAmask_bypass_set` | Only ASCII letters, digits, underscore |
| `firewall.whitelist_timeout` | `24h` | Positive whole seconds; used for substring-keyword target offload |
| `firewall.non_http.http_cooldown` | `1h` | Non-negative |
| `firewall.non_http.cleanup_interval` | `10m` | Positive |
| `observe.stats_file` | `/tmp/UAmask.stats` | Stats reporter destination |
| `observe.stats_interval` | `5s` | Positive; current UI does not poll it dynamically |

The JSON parser rejects unknown fields, duplicate fields, nulls, trailing JSON values, invalid durations, invalid modes/profiles, and unsupported schema versions. It trims and removes empty array strings, lowercases backend/log level, compiles regex only in regex mode, and writes effective output with the same schema.

## Non-persistent Lua CBI fields

These four current UI fields are not UCI options and never enter generated JSON:

| CBI field | Type | Current behavior |
|---|---|---|
| `status` | Read-only `DummyValue` | Executes `pidof UAmask`; renders raw red/green HTML for stopped/running |
| `stats_display` | Read-only `DummyValue` | Reads fixed `/tmp/UAmask.stats` once and renders nine metrics as raw HTML, or a stopped message |
| `log_display` | Read-only `TextValue` | Executes a shell `tail -n 200` command built from `main.log_file` |
| `clear_log` | `Button` | Executes a shell truncation command for `main.log_file` when the path exists; no confirmation |

## Exact rewrite and offload semantics

Evaluation order for a non-empty cleartext HTTP `User-Agent` is:

1. **Offload keyword** (`Firewall_ua_whitelist`): case-sensitive substring match. It takes precedence, does not rewrite the UA, is not cached, records an offload hit for the target IP:port, uses a 24-hour target timeout, and optionally drops the current connection.
2. **Exact pass-through** (`whitelist`): case-sensitive equality against the entire UA. It does not rewrite and is cached.
3. **All mode**: rewrites every detected non-empty cleartext HTTP UA. It does not decrypt or affect HTTPS headers.
4. **Regex mode**: case-insensitive Go RE2 match. Full mode replaces the entire UA; partial mode replaces matched portions.
5. **Keyword mode**: case-sensitive substring match and full-UA replacement.

An empty/missing User-Agent is passed through and not cached. Non-HTTP traffic does not enter the HTTP rewrite engine. Non-HTTP offload is a separate target policy requiring the enabled flag, threshold, decision delay, and absence of an active HTTP cooldown.

## Current lifecycle and failure visibility

| Operation/state | Current behavior | Failure visibility and contract status |
|---|---|---|
| Save without Apply | CBI writes UCI only; the custom action block does not call the service | No UI confirmation beyond normal LuCI behavior |
| Save & Apply, enabled form value `1`, `pidof` non-empty | Lua invokes `/etc/init.d/UAmask reload` | Stdout/stderr discarded; exit status ignored. Exact rc.common reload behavior is not locked by tests |
| Save & Apply, enabled form value `1`, no PID | Lua invokes `/etc/init.d/UAmask start` | Stdout/stderr discarded; exit status ignored |
| Save & Apply, enabled form not `1` | Lua invokes `/etc/init.d/UAmask stop` | Stdout/stderr discarded; exit status ignored |
| `start_service` while disabled | Returns failure before JSON/procd/firewall setup | Only service/log callers can observe it |
| Valid start | Optionally creates proxy group; generates and validates JSON; opens one named procd instance; runs `UAmask -config /var/run/UAmask/config.json`; watches config file; sets nofile, respawn, stdout/stderr; then installs firewall rules | Covered only for the basic enabled start path |
| Invalid JSON candidate | Logs core validation output, removes temp file, preserves prior `config.json`, and refuses start before procd/firewall setup | Error exists only in system log; no persisted last-error artifact and no actionable CBI message |
| Stop | `stop_service` removes firewall rules; procd owns process termination | UI cannot distinguish clean stop from failure |
| UCI reload trigger | `service_triggers` registers `procd_add_reload_trigger UAmask` | Trigger/reload transaction ordering and healthy-process preservation are not covered |
| Runtime status | CBI executes `pidof UAmask` and shows only red “未运行” or green “运行中” | Cannot represent disabled, starting, failed, stale PID, validation error, or firewall/backend state |

The candidate JSON is created in the destination directory with mode 0600 and renamed over the prior file after core validation. Serialization, validation, chmod, and rename failures remove the candidate. Directory creation happens before validation. The existing tests prove invalid generation preserves prior JSON; they do not prove that reload cannot stop a healthy process before validation.

## Current package and installed assets

`openwrt/package/Makefile` currently declares:

- package `UAmask`, version from repository `VERSION`, release 1;
- dependencies `luci-compat` and `libubox` plus architecture dependencies for source builds;
- conflict with `ua3f-tproxy`, `ua3f-tproxy-ipt`, and `UAmask-ipt`;
- `/etc/config/UAmask` as a conffile;
- source-build and prebuilt-binary installation paths.

Installed assets are exactly:

```text
/usr/bin/UAmask
/etc/init.d/UAmask
/etc/config/UAmask
/usr/lib/lua/luci/model/cbi/UAmask.lua
/usr/lib/lua/luci/controller/UAmask.lua
```

There are currently no native JavaScript views, `menu.d` metadata, rpcd ACL metadata, or constrained UI adapter. The Lua controller registers `admin/services/UAmask` and routes it to the single CBI model. Lua files and `luci-compat` are retained until `LUI-060` by confirmed migration policy.

## Current user-visible behavior and defects

- A single CBI page contains status, all settings, advanced tuning, statistics, and logs across four tabs.
- Version `0.4.3` is hard-coded in Lua rather than read from the installed binary/package.
- Statistics are parsed from fixed `/tmp/UAmask.stats` and rendered as text, but the page has no five-second client polling.
- Performance internals are the first general setting, although only custom users need them.
- Devices, ports, addresses, and all three logical string lists require handwritten separators.
- “修改所有流量” overstates the real scope: only detected cleartext HTTP User-Agent headers can be modified.
- The exact pass-through list and substring target-offload list are both called variants of “白名单,” despite materially different behavior and risk.
- Enabling set offload can bypass UA-Mask for a target and expose the original UA; disconnect-on-match can disrupt applications. Current copy does not require informed confirmation.
- Log viewing runs `tail` through a shell command built from the configured log path; clearing constructs another shell command. Quoting is insufficient for a path containing a double quote, so this is a command-injection boundary defect.
- Clearing logs has no confirmation. Read/clear/start/reload failures are hidden.
- Raw HTML, hard-coded red/green status, inline CSS, and PID-only detection do not provide accessible multi-state semantics.

These are product/security defects to replace, not compatibility behavior to reproduce in the JavaScript UI.

## Discrepancy classification and confirmed decisions

### Compatibility contracts

- Preserve all 28 UCI option names and the two named sections.
- Preserve schema-v1 JSON field names/types and `UAmask -config <path>`.
- Accept legacy CSV and whitespace-separated scalar values, including empty-value behavior.
- Preserve exact-UA pass-through versus substring-UA offload semantics.
- Preserve the OpenWrt port, profile, firewall-backend, fixed set, stats cadence, and safety-floor behavior unless a later reviewed task explicitly changes it.
- Preserve atomic candidate validation and last-valid JSON retention.

### Intentional OpenWrt overrides

- Port 12032 overrides the core library default 8080.
- Medium profile is the OpenWrt product default and resolves to cache 3000, buffer 8192, pool 500, GOGC 100.
- Firewall backend is detected at runtime instead of using the core default `ipt`.
- Init safety floors are stricter than the core for decision delay and target timeout.

### Product defects

- Missing-value defaults drift for cache size, keywords, regex, bypass ports/IPs, log path, port, and profile.
- Lifecycle/action errors are suppressed and not persisted for diagnostics.
- PID-only state, hard-coded version, non-polling stats, unsafe log shell interpolation, missing clear confirmation, separator-heavy inputs, and ambiguous/dangerous labels violate confirmed product requirements.

### Unresolved facts for later main review

1. The exact OpenWrt rc.common/procd behavior of the current explicit `reload` call and UCI reload trigger is not covered; `LUI-001` should lock only observable baseline assumptions that can be tested without production changes, and `LUI-011` must define the target transaction explicitly.
2. Current libubox behavior for reading canonical UCI `list` values through `config_get` is not an established contract. `LUI-010` must add explicit list iteration rather than rely on incidental joining.
3. Save-and-Apply ordering between CBI persistence and the custom inline service call is not proven by current tests. It must not be treated as a safe lifecycle contract.
4. Whether absent bypass-port/address options should inherit shipped defaults or remain empty is a product-default alignment decision for `LUI-010`; existing installed scalar values must remain unchanged either way.

## Regression evidence required by LUI-001

The unchanged baseline test task has enough information to cover:

- all 28 UCI keys being either mapped to JSON or explicitly classified as service/firewall-only;
- CSV trimming, empty elements, Unicode, quotes, and backslashes;
- whitespace-separated device, port, and IPv4/CIDR scalar fixtures and empty inputs;
- schema-v1 exact field/type output and fixed generated values;
- missing-value defaults, profile normalization/overrides, safety floors, mode mapping, regex failure, and core validation;
- disabled/enabled start, one procd instance, exact command/config/file/respawn parameters, and firewall ordering currently observable in the host-safe stub;
- invalid-candidate cleanup and preservation of the previous effective JSON;
- current package dependency and installed Lua asset assumptions, while deferring removal assertions to `LUI-060`.

No production, Lua, UCI, init, package, core, or test implementation was changed while producing this inventory.
