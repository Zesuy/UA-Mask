# LuCI JavaScript Migration Workflow

This directory is the durable source of truth for the UA-Mask Lua-to-JavaScript migration. It records confirmed product requirements, execution sequencing, bounded task cards, verification gates, and controller decisions.

## Source hierarchy

When information conflicts, use this order:

1. The user's latest explicit instruction.
2. Root `AGENTS.md` and any deeper applicable `AGENTS.md`.
3. `product_requirements.md` for confirmed product and compatibility requirements.
4. `decision_log.md` for reviewed implementation decisions.
5. `plan.md` for phase sequencing and risk control.
6. `tasks.md` and individual task cards for execution scope.
7. `verification.md` for quality gates.

Do not duplicate product facts into ad hoc notes. Amend the relevant source file through the main controller.

## Workflow

1. Main selects a `READY` task whose dependencies are `DONE`.
2. Main marks it `IN_PROGRESS` in the board and task card.
3. Main executes architectural tasks itself or invokes `luna_worker` with one self-contained card.
4. Worker reports but does not update central status.
5. Main inspects the diff, runs independent verification, and records review results.
6. Main marks the task `DONE` or returns focused feedback.
7. Main unblocks downstream tasks and updates `project_progress.md` and `latest_session_work.md`.

At most three page tasks may run concurrently, and only after shared compatibility and adapter work is stable. Any work touching `openwrt/root/etc/init.d/UAmask`, the status/log adapter, `openwrt/package/Makefile`, or UCI normalization is serialized.

## Task files

- Board: `agent_docs/workflow/tasks.md`
- Cards: `agent_docs/workflow/tasks/LUI-*.md`
- Current state: `agent_docs/project_progress.md`
- Session recovery: `agent_docs/latest_session_work.md`

The first executable task is `LUI-000`.
