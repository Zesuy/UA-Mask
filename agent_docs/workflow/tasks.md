# Workflow foundation task board

本分支基于 main，负责 CI、精确包运行验证与发布门禁。此前 LuCI/共存任务仍保留在其开发分支，本板不重置其状态。

| ID | Owner | Status | Dependencies | Card |
|---|---|---|---|---|
| CI-010 | worker (6.1 Sol fallback) | REVIEW | — | [Exact-package runtime smoke](tasks/CI-010.md) |
| CI-020 | main | IN_PROGRESS | — | Workflow integration, failure evidence, release gates and documentation |
| CI-030 | main | BLOCKED | CI-010, CI-020 | Independent local/cloud validation and draft PR delivery |

2026-10-22 主控已独立通过 core ci、14 项 Python 契约检查与 actionlint 1.7.7。CI-010 的真实 QEMU 验证和 CI-020 的云端集成结果待记录。
