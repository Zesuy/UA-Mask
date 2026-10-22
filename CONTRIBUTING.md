# 贡献与验证

请从当前 `main` 创建分支，关联所解决的 Issue，并在 PR 中说明触发条件、改动后的行为和验证结果。现有未合并功能应分别提交，避免将界面迁移、共存改造和 CI 基础设施混在同一个 PR。

本地先执行：

```sh
make -C core ci
python3 scripts/verify-release.py --check-matrix
python3 -m unittest discover -s scripts -p 'test_*.py' -v
python3 -m unittest discover -s openwrt/tests/runtime-smoke -p 'test_*.py' -v
```

涉及并发或流量处理时还应执行 `make -C core stress`。请遵循相应目录的 AGENTS.md 约束，保留 JSON schema v1、已有 UCI 名称和旧格式兼容性。

PR 的 Core CI 验证 Go 质量、覆盖率、race/stress、OpenWrt 配置与 procd 契约、跨架构编译，以及 OpenWrt 23.05.5/IPK 和 25.12.5/APK 的精确包构建与虚拟机运行。文档 PR 也会生成稳定的 `test-core` 汇总检查，便于配置分支保护。

首次贡献者的运行可能显示 `action_required`：这是 GitHub 对外部贡献运行的人工批准要求。维护者须先检查修改的 workflow 和代码，再批准；不要为此改用执行外部分支的 `pull_request_target` 或向 PR 授予发布权限。

失败时先查看对应 job 摘要，再下载测试证据。报告中提供运行链接、commit、OpenWrt 版本、包 hash 和失败阶段；分享前删除自己的地址、账号、密钥和网络配置。不要在真实路由器上直接运行测试脚本。详细重现和发布步骤见 [开发工作流](docs/development-workflow.md)。
