# OpenWrt 集成与构建

## 目录布局

- `package/`：OpenWrt `Makefile`，从 core 源码或预编译二进制生成 `UAmask` 包。
- `root/etc/config/UAmask`：默认 UCI 配置。
- `root/etc/init.d/UAmask`：配置生成、core 校验、procd 生命周期和防火墙管理。
- `root/usr/libexec/uamask-luci`：只接受固定动作的 LuCI 诊断适配器。
- `luci/htdocs/`：Overview、Settings、Diagnostics 三个原生 LuCI JavaScript 视图。
- `luci/root/usr/share/luci/menu.d/`：小写 LuCI 路由菜单元数据。
- `luci/root/usr/share/rpcd/acl.d/`：只授权 `UAmask` UCI、固定适配器和 init 脚本的 ACL。
- `tests/`：配置兼容、生命周期、适配器、JavaScript 和包资产契约测试。

包安装以下运行资产：

```text
/usr/bin/UAmask
/etc/init.d/UAmask
/etc/config/UAmask
/usr/libexec/uamask-luci
/usr/share/luci/menu.d/luci-app-uamask.json
/usr/share/rpcd/acl.d/luci-app-uamask.json
/www/luci-static/resources/view/uamask/overview.js
/www/luci-static/resources/view/uamask/settings.js
/www/luci-static/resources/view/uamask/diagnostics.js
```

旧 LuCI controller/CBI Lua 文件不会打包，依赖为 `luci-base` 和 `libubox`，不依赖 `luci-compat`。

## 运行时契约

`/etc/config/UAmask` 始终是用户配置真相源。init 脚本接受既有 CSV/空格分隔标量和规范 UCI `list`，生成 schema-v1 `/var/run/UAmask/config.json`，调用 core 校验候选文件并原子替换。成功后以以下命令交给 procd：

```sh
/usr/bin/UAmask -config /var/run/UAmask/config.json
```

候选配置无效时，正在运行的健康进程和上一份有效 JSON 会保留，错误写入固定诊断记录。LuCI 的 Save & Apply 只发起一次 `reload`，由 init 脚本决定启动、重载或停止。

稳定路由为：

```text
admin/services/uamask/overview
admin/services/uamask/settings
admin/services/uamask/diagnostics
```

## 构建模式

OpenWrt 包支持两种模式：

- 默认模式：使用 OpenWrt Go package infrastructure 从 `core/` 编译。
- 预编译模式：设置 `UAMASK_PREBUILT=/absolute/path/to/UAmask`，SDK 只生成与目标匹配的 IPK/APK 和安装资产。

发布与 Canary 构建统一从仓库根目录运行：

```sh
./scripts/build-release.sh \
  --platform openwrt \
  --arch arm64 \
  --sdk-dir /path/to/openwrt-sdk \
  --openwrt-version 23.05.5 \
  --target armsr \
  --subtarget armv8 \
  --package-arch aarch64_generic \
  --package-format ipk \
  --version 0.4.3-canary.example \
  --output-dir dist
```

OpenWrt 25.12.5 使用相同流程，但必须传入该版本对应的 SDK、target/subtarget/package architecture，并设置 `--package-format apk`。

脚本生成包、`.sha256` 和 `.metadata.json`。SDK 必须事先下载并校验；构建过程不会隐式下载工具链。脚本默认拒绝 dirty worktree，`--allow-dirty` 只适合本地调试并会在 metadata 中记录；可交付 Canary 和 release 不应使用它。

已核验 SDK 的 URL、SHA256、target/subtarget、Go 架构和包格式在 [SDK 矩阵](../.github/openwrt-sdk-matrix.json) 中：OpenWrt 23.05.5 生成 IPK，OpenWrt 25.12.5 生成 APK。

## 本地验证

```sh
make -C core ci
cd core && go test -race -count=1 ./...
sh openwrt/tests/luci-js-contract-test.sh
sh openwrt/tests/package-assets-test.sh
```

包产物还应核对依赖、架构、权限和内容，确认三个 JavaScript 视图、菜单、ACL、适配器、binary、init 与 UCI 配置存在，且 Lua controller/CBI 与 `luci-compat` 不存在。

## LuCI Playwright 产品检查

`openwrt/tests/e2e/` 提供无人值守的 OpenWrt 23.05.5 x86/64 QEMU 与 Playwright 检查。它只使用经过 SHA256 校验的官方 ext4 combined 镜像，首次准备一个带 LuCI、DHCP 管理口和测试 SSH key 的指纹化 base；每次运行都创建独立 qcow2 overlay，并使用 `restrict=on` 的 QEMU user networking 和 loopback-only SSH/HTTP 转发。

主机前置条件：Node.js 20 或更高版本、npm、QEMU x86_64、`qemu-img`、OpenSSH client、curl、gzip、netcat、`ss` 和 Chromium。默认使用 `/dev/kvm`（可用时），否则自动回退 TCG。首次安装 Node 依赖不会下载浏览器：

```sh
make luci-e2e-install
make luci-e2e-lint
make luci-e2e-prepare
```

本机可以复用系统 Chromium，例如：

```sh
CHROMIUM_PATH=/snap/bin/chromium make luci-e2e-smoke
CHROMIUM_PATH=/snap/bin/chromium make luci-e2e
```

快速 source 模式直接从当前 worktree 构建 core 并复制 config/init/adapter/menu/ACL/views，不构建或安装 IPK。完整模式覆盖三页、状态与恢复、五秒轮询、旧 UCI 列表、保存生命周期、失败保留、诊断操作、键盘、主题、布局和聚焦的 axe 检查。

axe 仅排除 LuCI 23.05 `form.Flag` 生成的、带 `data-widget-id="widget.cbid.UAmask.…"` 的内部 checkbox 节点：该版本为这些节点生成空的内部 `<label>`，字段标题由外层 CBI 结构显示。排除范围不覆盖文本、选择、列表、按钮、状态或告警，也不关闭任何 axe rule；Playwright 仍单独验证 Flag 的键盘 Space 操作、焦点和恢复。若 LuCI 后续提供可访问名称，应移除此精确排除。

精确 package 模式必须显式指定一个 IPK：

```sh
CHROMIUM_PATH=/snap/bin/chromium \
  make luci-e2e-package IPK=/absolute/path/to/UAmask_0.4.3-1_x86_64.ipk
```

运行前会验证包名、SHA256、必要资产、无 Lua 兼容文件，以及包内三个视图与当前期望源码一致；安装后再核对 guest 中的视图哈希。package 模式不会用 source copy 替代包安装。

默认输出位于 `dev/luci-e2e/<run-id>/`。失败会保留 Playwright trace、截图、HTML report、精简 console/network telemetry、QEMU serial、fixture action、source/package/OpenWrt/browser identity 和清理结果；不录制视频，也不保存 cookie 或 storage state。查看 trace：

```sh
cd openwrt/tests/e2e
npx playwright show-trace ../../../dev/luci-e2e/<run-id>/test-results/**/trace.zip
```

所有入口都验证只终止自己 overlay 对应的 QEMU PID，并检查运行目录对应的 Chromium 临时进程已退出。若需并行运行，必须为每个 worker 单独分配 VM 与转发端口；当前固定 `workers: 1`。
