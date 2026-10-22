# Workflow foundation — 2026-10-22

隔离分支 `codex/workflow-foundation` 基于 main 83846d3，草稿 PR [#28](https://github.com/Zesuy/UA-Mask/pull/28)。原始工作区、LuCI JS PR #25、共存分支与 Munet 调研文件未改动。

已实现并本地验证：双包格式 SDK smoke、官方全系统 QEMU 精确包验证、阶段事件与失败证据、清理门禁、可复用 Core CI、发布核心验证与完整工件审计、每周检查、贡献说明和 Actions 依赖更新。同行工作流来源与取舍见 [开发工作流](../docs/development-workflow.md)。

本地：core ci / full race / stress 通过；16 项 Python 检查通过；actionlint 1.7.7 通过。23.05.5 IPK 与 25.12.5 APK 的安装、版本、启停/重启、procd/PID/监听、配置校验和 LuCI 未认证入口均通过；故意在 boot 后失败保留 FAIL 和非零退出，清理三项全通过。

云端：正在执行当前分支 PR 验证与 artifacts-only 的完整候选发布。早期有缺陷的运行已由后续修复替代或取消；不能据此宣称当前 HEAD 已绿。最终结论与运行链接将更新到 PR 验证记录。

未执行合并、正式 tag/Release、外部 PR 的批准或实机操作。main 尚无分支保护，文档给出了待维护者配置的必需检查；没有擅自修改服务器治理设置。当前 smoke 不覆盖透明数据面、多项目共存、登录后浏览器交互或硬件。
