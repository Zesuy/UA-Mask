# UA-Mask Agent Workflow

This repository uses a main-controller to bounded-worker workflow for the LuCI JavaScript migration. The authoritative workflow lives under `agent_docs/workflow/`.

## Required reading order

Before doing migration work:

1. Read this file.
2. Read `agent_docs/latest_session_work.md`.
3. Read `agent_docs/project_progress.md`.
4. Read `agent_docs/workflow/product_requirements.md`.
5. Read `agent_docs/workflow/plan.md`.
6. Read `agent_docs/workflow/tasks.md`.
7. Read the selected task card completely.

When working under `core/`, also follow `core/AGENTS.md`. The deeper file takes precedence for core-specific architecture and testing rules.

## Product and compatibility invariants

- Preserve the single production path described in `docs/architecture.md`.
- Preserve all existing UCI option names and accept legacy scalar CSV/space-separated values.
- Preserve JSON schema version 1 and `UAmask -config <path>`.
- Support OpenWrt 23.05.5 and 25.12.5.
- Complete compatibility migration before interaction redesign.
- Use native LuCI JavaScript, native theme behavior, and light/dark compatibility.
- Do not introduce a frontend framework, charting library, Google Fonts, or a separate visual system.
- Do not operate a real firewall, real router, or physical device without explicit user authorization.

## Sol-to-Luna Max protocol

The primary agent is the controller (`main`). The project-scoped execution agent is `luna_worker`.

### Main controller responsibilities

- Own requirements, architecture, public interfaces, task decomposition, scheduling, conflict resolution, integration, and final review.
- Select only tasks whose status is `READY` and whose dependencies are complete.
- Give a worker exactly one complete task card per invocation.
- Ensure parallel tasks have disjoint writable file sets. Run shared `init`, adapter, package, UCI, and schema work serially.
- Run at most three non-conflicting worker tasks concurrently.
- After every worker result, inspect the diff, run the task's verification commands, resolve integration issues, and update the central state files.
- Be the only agent allowed to update `agent_docs/project_progress.md`, `agent_docs/latest_session_work.md`, `agent_docs/workflow/tasks.md`, task-card status, or `agent_docs/workflow/decision_log.md`.
- Keep architectural and cross-module work assigned to `main`.

### Luna worker responsibilities

- Execute only a self-contained task card assigned by the main controller.
- Do not create or delegate to subagents.
- Do not edit any file outside the task card's `允许修改的文件` section.
- Treat every `禁止修改的文件` entry as absolute.
- Read only the task card's required background plus repository instructions that apply to authorized files.
- Do not update central progress, task status, decision logs, or other task cards.
- Run every validation command listed in the task card before reporting completion.
- Report changed files, validation results, risks, and unfinished work using the task card's required format.
- After two failed implementation attempts for the same blocker, stop and escalate to the main controller.

## Task state protocol

Allowed task states are `BLOCKED`, `READY`, `IN_PROGRESS`, `REVIEW`, `DONE`, and `FAILED`.

- Only `main` changes task state.
- A task becomes `READY` only when all declared dependencies are `DONE` and its writable files do not overlap active work.
- A worker-completed task moves to `REVIEW`; it becomes `DONE` only after main inspects the diff and independently validates it.
- Failed verification does not expand task scope. Main either returns the same task with focused feedback or records a new integration task.

## Code exploration and editing

- Use CodeGraph first for structural questions such as symbol definitions, call paths, and impact.
- Use `rg` for literal strings and file enumeration.
- Trust fresh CodeGraph results; do not repeat them with grep.
- Preserve unrelated user changes and never reset, stash, or clean them without explicit approval.
- Use `apply_patch` for file edits.

## Required validation

Use the commands and phase gates in `agent_docs/workflow/verification.md`. Core changes additionally require the deterministic testing rules in `core/AGENTS.md`.
