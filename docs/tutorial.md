# UA-Mask 使用教程

本文面向 OpenWrt 用户，说明安装、原生 LuCI JavaScript 界面、匹配语义、目标卸载风险和排错方法。

## 先了解作用范围

UA-Mask 只处理检测到的明文 HTTP `User-Agent` 请求头：

- 它不能解密 HTTPS，也不会修改 TLS 内部的 HTTP 请求头。
- 缺少 User-Agent 的请求直接放行。
- 非 HTTP 流量不进入 UA 改写引擎。
- 端口或地址绕过、非 HTTP 卸载、UA 关键词目标卸载都会让后续连接避开 UA-Mask，可能直接暴露原始 UA。
- “断开当前连接”会故意中断命中的连接，让客户端重新连接到卸载路径，可能造成应用重试或会话中断。

因此，验证应使用你能控制的明文 HTTP 服务或抓包，而不能仅凭 HTTPS 网站页面判断。

## 快速开始

### 1. 安装匹配的包

当前固定 SDK 矩阵覆盖 OpenWrt 23.05.5 IPK 和 OpenWrt 25.12.5 APK。包格式、系统代际和设备架构必须全部匹配。

OpenWrt 23.05.5：

```sh
opkg install /tmp/UAmask_*.ipk
```

OpenWrt 25.12.5：

```sh
apk add --allow-untrusted /tmp/UAmask-*.apk
```

iptables 后端如需目标卸载，还要安装 `ipset`。默认依赖包括 `luci-base` 和 `libubox`，无需 `luci-compat`。

### 2. 完成基础设置

打开“服务 → UA-Mask → 设置”：

1. 勾选“Enable UA-Mask”。
2. 在 Basics 中选择 Performance profile，并填写 Replacement User-Agent。
3. 在 User-Agent matching 中选择 Match mode。初次使用建议保留 Keyword substring。
4. 在 Traffic scope 中确认入口设备、监听端口和绕过范围。
5. 保持 Smart offload 关闭，先完成基础验证。
6. 点击“保存并应用”。页面会先保存 UCI，再由 core 校验候选配置，并执行一次生命周期 reload。

保存成功后到“概览”确认状态为 Running。若失败，上一份有效运行配置会保留；打开“诊断”查看最近错误。

### 3. 验证明文 HTTP

在 LAN 客户端向受控 HTTP 服务发起请求，确认服务端收到的 User-Agent 等于 Replacement User-Agent。也可在 WAN 侧抓取你有权检查的流量：

```sh
tcpdump -i <wan-device> -s 0 -A 'tcp port 80'
```

不要在命令中捕获无关用户流量；抓包文件可能包含敏感明文内容，用完后应妥善清理。

## 三个 LuCI 页面

### 概览

概览页每 5 秒发起一次非重叠状态刷新，并显示：

- Disabled、Starting、Running、Failed、Enabled but not running 等服务状态。
- 已安装版本和当前 nftables/iptables 后端。
- 当前连接、总请求、成功修改、直接放行、RPS、规则处理、修改/放行缓存命中和总缓存率。
- 最近配置或服务错误，以及无统计文件时的明确提示。
- 当前目标卸载、非 HTTP 绕过、UA 关键词卸载和断开连接风险提示。

刷新失败时页面保留上一次可用状态并提供重试，不应把旧数据误认为新数据。

### 设置

设置页分为 Basics、User-Agent matching、Traffic scope、Smart offload 和 Expert performance 五组。列表字段使用 LuCI DynamicList，入口设备使用 DeviceSelect，不再要求手写整串分隔符。

旧配置兼容规则：

- `keywords`、`whitelist`、`Firewall_ua_whitelist` 可读取旧逗号分隔标量。
- `iface`、`bypass_ports`、`bypass_ips` 可读取旧空格分隔标量。
- 成功保存会把当前有效列表规范化为 UCI `list`，生成的 schema-v1 JSON 语义保持一致。
- 空列表会保留为空，不会因删除字段而意外重新启用默认值。

“保存”只暂存/保存配置；“保存并应用”才校验并执行一次服务生命周期操作。

### 诊断

诊断页提供：

- 服务状态、PID、版本和防火墙后端。
- 最近 200 行应用日志，以及需要二次确认的清空日志操作。
- core 当前使用的 schema-v1 最终生效 JSON，只读展示。
- 最近一次配置校验或服务启动错误。
- 整页刷新、分区重试和清晰的缺失文件/异常状态提示。

浏览器不能指定文件路径或命令；页面只调用固定适配器动作。请在设置页修改 UCI，不要直接编辑 `/var/run/UAmask/config.json`。

## 设置字段与语义

### Basics

- `operating_profile`：Low、Medium、High 或 Custom。预设会决定 pool/cache 等运行参数；只有 Custom 才使用自定义性能字段。
- `ua`：命中后使用的 Replacement User-Agent。默认值为 `FFF`，正式使用前通常应改成你需要的完整值。

### User-Agent matching

- `match_mode=keywords`：对 User-Agent 做区分大小写的子串匹配，命中后整串替换。
- `match_mode=regex`：使用 Go RE2、大小写不敏感的正则；Full 替换整个 UA，Partial 只替换命中片段。
- `match_mode=all`：替换所有检测到的非空明文 HTTP User-Agent，不代表能修改 HTTPS 或其他协议。
- `keywords`：每项一个关键词子串，仅在 keywords 模式生效。
- `ua_regex`：regex 模式使用的 Go RE2 表达式。
- `replace_method`：regex 模式的 Full 或 Partial 替换范围。
- `whitelist`：精确 User-Agent 放行。只有完整字符串区分大小写相等时才直接放行，结果仍可缓存。

精确放行 `whitelist` 与 `Firewall_ua_whitelist` 必须区分：前者是“完整 UA 相等则本次请求不修改”，后者是“UA 包含关键词则把目标 IP:port 调度到卸载路径”。后者风险更高。

### Traffic scope

- `port`：透明重定向监听端口，范围 1–65535，默认 12032。
- `iface`：需要重定向的一个或多个 LAN 入口设备，默认 `br-lan`。
- `proxy_host`：是否也重定向路由器自身产生的合格 TCP 流量。与本机代理共存时需谨慎。
- `bypass_gid`：`proxy_host` 开启时豁免的 Linux GID，默认 65533。
- `bypass_ports`：重定向前直接豁免的目标 TCP 端口，默认配置含 22、443。
- `bypass_ips`：重定向前直接豁免的目标 IPv4 地址或 CIDR。

绕过端口/地址中的明文 HTTP 不会被 UA-Mask 修改。默认绕过 443 适合常规 HTTPS，但如果目标在 443 上提供明文 HTTP，它同样会被绕过。

### Smart offload

- `enable_firewall_set`：启用目标卸载总开关。匹配目标可能最长 24 小时不经过 UA-Mask，原始 UA 可能泄露。
- `Firewall_ua_bypass`：达到阈值并经过观察延迟后，将持续判定为非 HTTP 的目标卸载。
- `Firewall_ua_whitelist`：区分大小写的 UA 子串目标卸载列表。命中优先于精确放行和改写，不修改当前 UA，并把目标 IP:port 调度到卸载路径。
- `Firewall_drop_on_match`：命中目标卸载关键词时主动断开当前连接；客户端重连后可能直接走卸载路径。

启用卸载前先接受两个事实：目标绕过可能泄露原始 UA；断开匹配可能中断应用。若优先保证每个可检测明文 HTTP 请求都经过改写，应关闭目标卸载和 disconnect-on-match。

### Expert performance

- `firewall_advanced_settings`：允许调整非 HTTP 卸载判定参数。
- `firewall_nonhttp_threshold`：判定前的连续非 HTTP 连接数，最小 1，默认 5。
- `firewall_decision_delay`：达到阈值后的观察时间，单位秒，安全下限 10，默认 60。
- `firewall_timeout`：非 HTTP 目标卸载时长，单位秒，安全下限 60，默认 28800（8 小时）。
- `buffer_size`：Custom profile 的单连接缓冲区，1024–65536 字节。
- `pool_size`：Custom profile 的 worker 数；0 表示每连接一个 goroutine。
- `cache_size`：Custom profile 的决策缓存条目数。
- `gogc_value`：Go GC 百分比；`-1` 会关闭自动 GC，只适合明确了解影响的用户。
- `log_level`：debug、info、warn、error、fatal 或 panic。
- `log_file`：应用日志路径；留空关闭 core 文件日志。诊断适配器只接受 UA-Mask 的受限日志位置。

性能预设不是吞吐或 CPU 百分比保证。实际结果取决于路由器 CPU、内存、网络、规则和流量结构，应在自己的设备上基准测试。

## 与其他本机代理共存

UA-Mask 可以独立工作，也可以和 OpenClash 等代理同时安装，但防火墙顺序和本机流量范围取决于各自配置：

- 通常先关闭 UA-Mask 的 `proxy_host`，验证 LAN 流量工作后，再决定是否代理路由器本机流量。
- 需要 `proxy_host` 时，应确保 `bypass_gid` 和其他代理的运行身份不会形成重定向回环。
- 更改任一服务的防火墙设置后，重新检查链顺序和实际出口；不要假定“安装即完美分流”。

常用只读检查：

```sh
# iptables
iptables -t nat -S UAmask_prerouting
iptables -t nat -S UAmask_output
ipset list UAmask_bypass_set 2>/dev/null

# nftables/firewall4
fw4 print ruleset | sed -n '/UAmask_/,+20p'
nft list set inet fw4 UAmask_bypass_set 2>/dev/null
```

## 验证与排错

### 服务和配置

```sh
pidof UAmask || echo 'UAmask is not running'
logread -e UAmask | tail -n 50
ls -l /var/run/UAmask/config.json /var/run/UAmask/last_error 2>/dev/null
/usr/bin/UAmask -check-config -config /var/run/UAmask/config.json
/usr/bin/UAmask -dump-effective-config -config /var/run/UAmask/config.json
```

若新配置无效，Save & Apply 会报错，健康 PID 和上一份有效 JSON 应保持不变。修正设置后再次 Save & Apply；恢复成功后最近错误会清除。

### 常见现象

- HTTPS 看不到修改：正常；TLS 内部 UA 不可见。
- 概览暂时没有统计：core 每 5 秒写一次快照，启动后稍候；若持续缺失，到诊断页检查状态和错误。
- Enabled but not running：配置声明启用但未发现进程；检查最近错误、core 校验和日志。
- 防火墙后端缺少 set 支持：iptables 后端安装 `ipset`，再重新应用配置。
- 应用被断开：检查 `Firewall_drop_on_match`；它的设计行为就是主动断开命中连接。
- 发现原始 UA：检查目标端口/IP 绕过和 `UAmask_bypass_set`，并检查目标卸载、非 HTTP 绕过和关键词卸载是否启用。
- DeviceSelect 没有预期接口：确认 `/etc/config/network` 和相关设备存在；也可输入 LuCI 允许创建的设备名。

### 抓包检查原始 UA

先确定 WAN 设备：

```sh
WAN_DEVICE="$(uci -q get network.wan.device || uci -q get network.wan.ifname || ifstatus wan | jsonfilter -e '@.device')"
printf '%s\n' "$WAN_DEVICE"
```

只抓取短时间、已授权的明文 HTTP 样本：

```sh
timeout 30 tcpdump -i "$WAN_DEVICE" -s 0 -U -w /tmp/uamask-http.pcap \
  'tcp port 80 or tcp port 8080 or tcp port 8000'
```

在 Wireshark 中使用 `http.request and http.user_agent` 过滤。若确认泄露，先停用目标卸载和 disconnect-on-match，再清理或等待 `UAmask_bypass_set` 超时并复测。直接删除防火墙 set 元素会改变路由器状态，执行前应确认精确目标和当前后端。

## UCI 配置参考

已有选项名保持兼容。规范列表示例：

```conf
config 'UAmask' 'enabled'
    option enabled '1'

config 'UAmask' 'main'
    option port '12032'
    option ua 'Mozilla/5.0 ...'
    option operating_profile 'Medium'
    option match_mode 'keywords'
    list keywords 'Windows'
    list keywords 'Android'
    option replace_method 'full'
    list iface 'br-lan'
    option proxy_host '0'
    option bypass_gid '65533'
    list bypass_ports '22'
    list bypass_ports '443'
    list bypass_ips '192.168.0.0/16'
    list bypass_ips '127.0.0.0/8'
    option enable_firewall_set '0'
    option Firewall_ua_bypass '0'
    option Firewall_drop_on_match '0'
    option firewall_advanced_settings '0'
    option log_level 'info'
    option log_file '/tmp/UAmask/UAmask.log'
```

旧写法如 `option keywords 'Windows,Android'`、`option iface 'br-lan eth1'` 仍能读取，但建议通过 LuCI 保存一次，让有效列表自动规范化。不要改名 `Firewall_ua_whitelist` 等大小写敏感的既有键。

## 源码构建

使用仓库根目录的 `scripts/build-release.sh` 和经过校验的 OpenWrt SDK。OpenWrt 23.05.5 传 `--package-format ipk`，OpenWrt 25.12.5 传 `--package-format apk`。具体参数、SDK 矩阵和产物说明见 [`openwrt/README.md`](../openwrt/README.md)。

脚本会生成安装包、SHA256 和 metadata，默认拒绝 dirty worktree。不要从不匹配系统版本或架构的 SDK 构建可交付包。
