# 开发、验证与发布

这套流程基于当前 main 产品，先建立可运行的检查闭环。LuCI JS、透明数据面和多项目共存功能应在各自 PR 接入，不能由当前 smoke 的绿灯替代其验收。

## 触发与门禁

| 场景 | 执行内容 | 结果 |
|---|---|---|
| 每个 PR、main/master 推送 | Core CI：质量、单测/覆盖率、race/stress、OpenWrt 契约、9 种 Go 架构、两版 SDK 与 QEMU | `test-core` 汇总；包与证据分别保存 |
| 每周一 03:17 UTC、手动 Core CI | 同上，发现镜像源或依赖漂移 | 不创建 Release |
| 手动 OpenWrt SDK build | smoke 两目标或 all 全部 15 目标 | 工件；x86/64 两目标执行 QEMU |
| 手动 Release | 核心验证、9 个二进制、15 个包、两版 QEMU、完整性审计 | `release-assets` 候选工件，不发布 |
| 推送 `vVERSION` tag | 与手动 Release 相同，tag 必须匹配 VERSION | 所有门禁通过才发布 GitHub Release |

`test-core` 无 workflow 级路径过滤，因此文档 PR 也有明确检查结果。发布调用同一核心验证，但复用发布自身的跨架构和 SDK 构建，避免重复。job 失败、取消或意外跳过都不能被汇总为通过。

维护者可在新流程验证通过后，将 `Core CI / test-core` 设为 main 的必需状态检查，并要求 PR 合并。此文件不表示服务器已经配置了分支保护。发布仅由 tag 触发，不自动合并 PR、增加 tag 或批准外部贡献者的运行。

## 本地精确包验证

宿主需要 Go、Python 3.10+、QEMU x86 系统模拟器、qemu-img 和 OpenSSH 客户端。Linux 下可用 KVM 时自动使用，否则 TCG；实际选择写入 result。无需宿主 root、TAP、bridge、namespace 或防火墙修改。

先提交源码，再使用已准备的官方 SDK 构建（具体 SDK URL 与 SHA 固定在 `.github/openwrt-sdk-matrix.json`）：

```sh
./scripts/build-release.sh --platform openwrt --arch amd64 \
  --sdk-dir /absolute/path/to/extracted-sdk \
  --openwrt-version 23.05.5 --target x86 --subtarget 64 \
  --package-arch x86_64 --package-format ipk \
  --output-dir dist/local-ipk

python3 openwrt/tests/runtime-smoke/run.py --openwrt 23.05.5 \
  --package /absolute/path/to/UAmask.ipk \
  --metadata /absolute/path/to/UAmask.ipk.metadata.json \
  --cache-dir .cache/openwrt-images --run-dir .cache/runs/unique-ipk-run
```

25.12.5 使用其 SDK、`--package-format apk` 和对应 APK。`--commit` 若指定必须等于当前完整 HEAD，默认自动读取。构建拒绝脏工作区；`--allow-dirty` 只适合临时调试，正式工作流及发布审计不接受 dirty 工件。

runner 从固定 SHA256 的官方原始镜像启动，每次创建只读 base 与独立 overlay，仅使用绑定 127.0.0.1 的 QEMU user networking。网络、启动管理和防火墙操作仅在一次性 guest 内执行。缓存只保存下载内容，复用前重新校验。

检查包含包 hash/平台/source、guest 系统版本、正常依赖安装、已安装包及二进制版本、默认禁用、启用/停止/重启、procd/PID/监听、生成配置校验、非法配置拒绝，以及当前 LuCI 路由注册和未认证 HTTP 入口。uhttpd/LuCI 管理组件作为明确的测试夹具安装，未用强制忽略依赖选项。本地 SDK 包允许未受信签名，不表示正式签名链已经验证。

每次运行目录必须是新路径。`--image-file` 可复用本地官方压缩镜像，仍严格校验 SHA。加入 `--fail-after boot` 应得到非零退出，用来验证失败取证和清理，不能把该运行算作通过。

## 失败证据

SDK job 的 `evidence-<target>` 与可发布的 `openwrt-<target>` 分开保存。证据上传使用明确日志文件后缀，不上传虚拟磁盘和临时私钥。

- `result.json`：PASS/FAIL/ERROR、源码与包身份、失败阶段、错误和清理结果。
- `events.jsonl`：每阶段开始/结束/失败、UTC 时间与耗时。
- 串口、QEMU 错误、guest 系统身份、包安装及包数据库、服务、LuCI 和诊断日志。
- `build.log`：完整 SDK 构建输出；Core CI 另存覆盖率、测试事件、race/stress 与契约失败证据。

runner 清理自己创建的 QEMU、overlay 和临时 SSH 材料后才落最终结果。清理失败为 ERROR，禁止报 PASS。工作流被硬性取消或 runner 宕机时，GitHub 可能无法上传工件；此时不应推断 guest 场景已通过。

## 发布完整性

发布前 `scripts/verify-release.py` 要求 9 个 Go 架构与矩阵全部 15 个 OpenWrt 目标各出现一次，并检查所有工件的 commit、version、dirty=false、SHA256 与独立 checksum 文件。缺包、混合来源、重复平台、篡改、额外证据文件都会阻断。手动 canary 同样运行该检查，输出集合再用于正式发布，日志不会混进 release assets。

正式发布前先审阅候选包与验证记录，再按维护者决定更新 VERSION、提交并推送匹配 tag。本轮基础设施建设不会自行发正式版。

## 后续验收边界

当前 QEMU smoke 不能证明透明转发确实改写 UA、IPv6 转发、多设备拓扑、OpenClash/ShellCrash 共存、LuCI 登录后交互或真实硬件行为。下一层应复用这些精确包与证据格式，加入 client/router/server 拓扑、抓包、故障注入和浏览器流程。Munet 尚未作为本项目依赖引入；先让当前产品的检查和发布闭环稳定。

## 同行来源与取舍（2026-10-22 核实）

| 来源 | 实际做法 | 本项目采用 |
|---|---|---|
| [OpenWrt LuCI build.yml](https://github.com/openwrt/luci/blob/580b6b1d9ec3a79edb8e2a5a9c4700b9c320ff6a/.github/workflows/build.yml) | PR 经格式检查后调用共享工作流，指定 x86_64 | PR 门禁、复用工作流；不复制其大型 feeds 的审批链 |
| [OpenWrt packages](https://github.com/openwrt/packages/blob/c33185f91520a334d3a905a703a0cf1c33ababea/.github/workflows/multi-arch-test-build.yml) | 使用统一的 feeds 构建流程 | 同一验证定义用于 PR 和发布 |
| [官方共享流程](https://github.com/openwrt/actions-shared-workflows/blob/fb28bcc4fac29086c22e4fee8f8af57153e0b54c/.github/workflows/multi-arch-test-build.yml) | gh-action-sdk、多架构、IPK/APK 检测、始终保存包和日志；部分目标用 QEMU user emulation + Docker 测试 | 构建/证据分离、双包格式、矩阵 fail-fast=false；增加本项目需要的完整系统 QEMU/procd 验证 |
| [luci-app-mosdns 发布流程](https://github.com/sbwml/luci-app-mosdns/blob/6a781327e18d3a2dbe1ec1aacb63449633246ade/.github/workflows/release-build.yml) | tag 触发、架构 × OpenWrt 分支矩阵、SDK action、上传 release；单目标失败不取消其他目标 | 采用矩阵独立构建；本项目要求全目标成功和集合审计后集中发布，避免逐目标替换正式附件 |
| [SmartDNS 发布流程](https://github.com/pymumu/smartdns/blob/d3464827bb09c0a29fd5cb9e8e6926eb4820f9d6/.github/workflows/release.yml) | 定时/手动/正式发布复用交叉构建，缓存工具链，自动替换 nightly | 采用定时检查与缓存；本项目手动运行只产生候选工件，暂不自动删除或替换 nightly/tag |

官方共享 action 跟随 feeds 分支，适合大型包仓库。本项目已有 SHA 固定的两版发行 SDK，因此保留现有构建脚本，避免为接入 CI 改变发行来源。暂不引入另一个 CI 服务、通用实验平台或额外前端。
