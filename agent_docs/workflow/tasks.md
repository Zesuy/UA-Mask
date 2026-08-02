# LuCI JavaScript Migration Task Board

Only the main controller updates this board. `READY` means every dependency is `DONE` and no active task owns an overlapping file.

| ID | Phase | Title | Owner | Status | Dependencies | Card |
|---|---:|---|---|---|---|---|
| LUI-000 | 0 | Baseline contract and behavior inventory | main | DONE | — | [LUI-000](tasks/LUI-000.md) |
| LUI-001 | 0 | Lock legacy OpenWrt contracts in tests | luna_worker | DONE | LUI-000 | [LUI-001](tasks/LUI-001.md) |
| LUI-010 | 1 | Canonical UCI lists and explicit defaults | luna_worker | DONE | LUI-001 | [LUI-010](tasks/LUI-010.md) |
| LUI-011 | 1 | Transactional init/procd lifecycle | main | DONE | LUI-010 | [LUI-011](tasks/LUI-011.md) |
| LUI-012 | 1 | Compatibility migration integration review | main | DONE | LUI-011 | [LUI-012](tasks/LUI-012.md) |
| LUI-020 | 2 | Constrained status and diagnostics adapter | luna_worker | DONE | LUI-012 | [LUI-020](tasks/LUI-020.md) |
| LUI-021 | 2 | JavaScript menu, ACL, and package foundation | main | DONE | LUI-020 | [LUI-021](tasks/LUI-021.md) |
| LUI-030 | 3 | Overview page | luna_worker | DONE | LUI-021 | [LUI-030](tasks/LUI-030.md) |
| LUI-040 | 4 | Settings page | luna_worker | DONE | LUI-021, LUI-010 | [LUI-040](tasks/LUI-040.md) |
| LUI-050 | 5 | Diagnostics page | luna_worker | DONE | LUI-021 | [LUI-050](tasks/LUI-050.md) |
| LUI-060 | 6 | Replacement integration and Lua removal | main | DONE | LUI-012, LUI-030, LUI-040, LUI-050 | [LUI-060](tasks/LUI-060.md) |
| LUI-061 | 6 | Automated regression and package verification | main | DONE | LUI-060 | [LUI-061](tasks/LUI-061.md) |
| LUI-062 | 6 | OpenWrt 23.05 QEMU product smoke | main | DONE | LUI-061, LUI-063, LUI-064, LUI-065, LUI-066 | [LUI-062](tasks/LUI-062.md) |
| LUI-063 | 6 | OpenWrt 23.05 adapter nounset compatibility | luna_worker | DONE | LUI-060 | [LUI-063](tasks/LUI-063.md) |
| LUI-064 | 6 | Remove rendered null placeholders | luna_worker | DONE | LUI-060 | [LUI-064](tasks/LUI-064.md) |
| LUI-065 | 6 | DynamicList blank-editor Save & Apply compatibility | luna_worker | DONE | LUI-064 | [LUI-065](tasks/LUI-065.md) |
| LUI-066 | 6 | Normalize unchanged legacy scalars on save | luna_worker | DONE | LUI-065 | [LUI-066](tasks/LUI-066.md) |
| LUI-070 | 7 | Final integration and product review | main | DONE | LUI-061, LUI-062 | [LUI-070](tasks/LUI-070.md) |

## Parallelism rules

- `LUI-000` through `LUI-021` are serial because they establish shared contracts and infrastructure.
- After `LUI-021` is `DONE`, `LUI-030`, `LUI-040`, and `LUI-050` may run concurrently. Their authorized implementation files are disjoint.
- `LUI-060` is the only task allowed to remove the legacy Lua files.
- `LUI-062` cannot begin until all pages, lifecycle work, removal, and automated regression are complete.
- `LUI-063` is the bounded QEMU-discovered adapter compatibility correction; `LUI-062` resumes only after main reviews it.
- `LUI-064` removes QEMU-confirmed literal `null` DOM text without changing page behavior or interfaces.
- `LUI-065` is serialized after `LUI-064` because both strengthen the shared JavaScript contract test.
- `LUI-066` closes the QEMU-confirmed unchanged-value normalization gap after Save & Apply.
- No worker may update this board or unblock another task.

## Status transition

```text
BLOCKED → READY → IN_PROGRESS → REVIEW → DONE
                            ↘ FAILED
```

Main records review evidence before moving `REVIEW` to `DONE`.
