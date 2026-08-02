# UA-Mask

[![GitHub Release][release-shield]][release-url]
[![GPL-3.0 License][license-shield]][license-url]

UA-Mask 是面向 OpenWrt 的透明 HTTP User-Agent 修改工具。它通过防火墙把符合范围的 TCP 流量重定向到 Go 核心，并提供原生 LuCI JavaScript 界面完成配置、状态查看和诊断。

> [!IMPORTANT]
> UA-Mask 只能识别和修改明文 HTTP 中的 `User-Agent` 请求头，不能解密或修改 HTTPS 中的请求头。任何被绕过或卸载的目标都可能直接看到原始 User-Agent；不要把本项目当作“零泄露”保证。

## 功能

- 关键词、Go RE2 正则和“全部检测到的明文 HTTP UA”三种匹配模式。
- 精确 User-Agent 放行列表：只在完整字符串相等时放行，和目标卸载关键词不是同一功能。
- nftables/firewall4 与 iptables 后端，支持按设备、目标端口和 IPv4/CIDR 控制流量范围。
- 可选目标卸载：支持非 HTTP 判定和 UA 子串目标卸载，但启用后存在原始 UA 泄露风险。
- 原生 LuCI JavaScript 三页界面：概览、设置、诊断；不依赖 `luci-compat`。
- 状态与统计每 5 秒刷新；诊断页提供受限日志、最终生效配置、最近错误和确认后清空日志。
- 保留既有 UCI 选项名、JSON schema version 1 和 `UAmask -config <path>`；旧 CSV/空格分隔列表仍可读取，成功保存后转为规范 UCI `list`。

## 支持的 OpenWrt 包代际

当前固定 SDK 矩阵覆盖：

- OpenWrt 23.05.5：IPK 包，使用 `opkg`。
- OpenWrt 25.12.5：APK 包，使用 `apk`。

具体 target、subtarget、架构和 SDK 校验值见 [`.github/openwrt-sdk-matrix.json`](./.github/openwrt-sdk-matrix.json)。下载包时必须同时匹配 OpenWrt 代际和设备架构。

## 安装

从 [Releases][release-url] 下载匹配设备的包并复制到路由器，例如 `/tmp/`。

OpenWrt 23.05.5：

```sh
opkg install /tmp/UAmask_*.ipk
```

OpenWrt 25.12.5：

```sh
apk add --allow-untrusted /tmp/UAmask-*.apk
```

在 iptables 后端启用目标卸载前，还需要安装 `ipset`。包本身依赖 `luci-base` 和 `libubox`。

## 使用

安装后打开 LuCI 的“服务 → UA-Mask”：

1. 在“设置”页启用服务，选择匹配方式并填写 Replacement User-Agent。
2. 检查流量范围。默认监听端口为 `12032`，默认入口设备为 `br-lan`，默认绕过目标端口为 `22` 和 `443`。
3. 点击“保存并应用”。候选配置会先经 core 校验，再执行一次服务生命周期操作。
4. 在“概览”页确认状态与统计；失败时到“诊断”页查看最终生效 JSON 和最近错误。
5. 用受控的明文 HTTP 请求验证结果。HTTPS 看不到修改效果是预期行为。

目标卸载默认关闭。启用 `Target-offload User-Agent keywords` 会按区分大小写的子串匹配目标；该请求不会修改 UA，并可能让目标在最长 24 小时内绕过 UA-Mask。启用 `Disconnect current connection on keyword match` 还会主动断开当前连接，可能中断应用会话。

详细字段、风险和排错步骤见 [使用教程](./docs/tutorial.md)。

## 构建

仓库提供统一构建脚本，可生成 Linux 二进制，以及由指定 OpenWrt SDK 打包的 IPK/APK：

```sh
./scripts/build-release.sh --help
```

OpenWrt 构建需要预先准备并校验 SDK。脚本默认拒绝 dirty worktree，并为产物生成 SHA256 和 metadata。完整示例与两种包构建模式见 [OpenWrt 构建说明](./openwrt/README.md)。

## 仓库结构

- `core/`：Go 核心、命令入口和测试。
- `openwrt/package/`：OpenWrt 包定义。
- `openwrt/root/`：UCI 默认配置、init 脚本和受限 LuCI 适配器。
- `openwrt/luci/`：原生 JavaScript 视图、菜单和 ACL。
- `openwrt/tests/`：配置、生命周期、适配器、LuCI 和包资产契约测试。
- `docs/`：教程、架构和配置示例。

运行时只有一条生产路径：`/etc/config/UAmask` 是用户配置真相源，init 脚本生成并校验 `/var/run/UAmask/config.json`，然后以 `/usr/bin/UAmask -config /var/run/UAmask/config.json` 启动 core。详见 [架构说明](./docs/architecture.md)。

## 与 UA3F 的关系

本项目最初源于 [UA3F](https://github.com/SunBK201/UA3F)，并在连接处理、配置、缓存、OpenWrt 生命周期和目标卸载等方面进行了重构。感谢上游项目及贡献者；若归属标注有遗漏，欢迎提交 Issue 或 PR。

## 许可证

本项目使用 [GNU GPL v3](./LICENSE)。

[release-shield]: https://img.shields.io/github/v/release/Zesuy/UA-Mask?style=flat
[release-url]: https://github.com/Zesuy/UA-Mask/releases
[license-shield]: https://img.shields.io/github/license/Zesuy/UA-Mask.svg?style=flat
[license-url]: https://github.com/Zesuy/UA-Mask/blob/main/LICENSE
