# OpenWrt Layout

- `package/`：OpenWrt `Makefile`，负责把 `core/` 编译为可安装包。
- `root/`：打包时直接铺到目标系统根目录的文件，例如 `etc/init.d/` 和 `etc/config/`。
- `luci/`：LuCI 控制器与 CBI 页面。

运行时仍以 `/etc/config/UAmask` 为用户配置真相源；`init.d` 会将 core 所需字段转换成 `/var/run/UAmask/config.json`，校验通过后使用 `UAmask -config <path>` 启动。

## 构建模式

OpenWrt 包保留两种构建模式：

- 默认模式由 OpenWrt 的 Go package infrastructure 从 `core/` 编译。
- `UAMASK_PREBUILT=/absolute/path/to/UAmask` 使用已经交叉编译的静态二进制，SDK 只负责生成与目标架构匹配的 IPK/APK 元数据和安装包。

发布和 Canary 构建统一从仓库根目录执行：

```sh
./scripts/build-release.sh \
  --platform openwrt \
  --arch arm64 \
  --sdk-dir /path/to/openwrt-sdk \
  --version 0.4.3-canary.example \
  --output-dir dist
```

脚本会同时生成包、`.sha256` 和 `.metadata.json`。SDK 必须事先下载并校验；仓库不会在构建过程中隐式下载未固定的工具链。

为避免 artifact 与 commit 对不上，脚本默认拒绝 dirty worktree。`--allow-dirty` 只用于本地调试，并会在 metadata 中明确记录 `"dirty": true`；可交付 Canary 和 release 不得使用该参数。

已经核验的 SDK URL、SHA256、target/subtarget、Go 架构和包格式记录在 `.github/openwrt-sdk-matrix.json`。首个 Canary 条目是 OpenWrt 23.05.5 `x86/64`，同时适用于对应的 x86_64 QEMU 和实体机。
