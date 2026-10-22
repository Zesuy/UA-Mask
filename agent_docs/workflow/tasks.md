# Workflow foundation task board

本分支基于 main，负责 CI、精确包运行验证与发布门禁。此前 LuCI/共存任务仍保留在其开发分支，本板不重置其状态。

| ID | Owner | Status | Dependencies | Card |
|---|---|---|---|---|
| CI-010 | worker (6.1 Sol fallback), integrated by main | DONE | — | [Exact-package runtime smoke](tasks/CI-010.md) |
| CI-020 | main | DONE | — | Workflow integration, failure evidence, release gates and documentation |
| CI-030 | main | IN_PROGRESS | CI-010, CI-020 | Independent local/cloud validation and draft PR delivery |

2026-10-22 主控独立通过 core ci、full race、20 轮 deterministic stress、16 项 Python 契约检查与 actionlint 1.7.7。实际 SDK IPK/APK 均已在对应官方镜像通过安装、服务生命周期、配置与 LuCI 入口检查；故意 boot 后失败的非零退出、证据与清理已验证。云端完整集成结果待 CI-030 记录。草稿 PR：https://github.com/Zesuy/UA-Mask/pull/28。
