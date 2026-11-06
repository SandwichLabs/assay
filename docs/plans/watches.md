# Watches and alerts (E7, #247, phase 0)

A **watch** is a standing question with a condition. Checking it runs the question once through the person's read-only path. If the condition holds, it raises an **alert**. Phase 0 has no scheduler, no background service and no delivery. A watch is checked only when the person asks: `slaw watch check` or Studio's Check button. A triggered alert is reported where the check ran, and every check's outcome is kept in a history that never leaves this machine. Scheduling, `slaw watch serve`, desktop notifications, digests and per-person delivery are later phases of #247.

## Owner decisions (2026-11-06)

- **On demand only.** Nothing runs by itself. A cloned workspace's watches therefore need no enable step yet, as with `slaw semantics check` today. The enable gate (trustfile, like `.env` and views) lands with scheduling, because that is when a watch first runs without a person asking.
- **Two conditions:** a threshold on one value, and "any rows" (or "no rows").
- **Report and local history:**
  - The CLI prints a summary and exits non-zero when an alert fired.
  - Studio shows the alerts and a badge on the nav.
  - Each check is logged in `.slaw-local/watches/`, never in the folder, git or a bundle.

## The file: `watches/<name>.yaml`

`<name>` follows the connection-name rule (`internal/connname`). The file is decoded with `internal/strictyaml`: a byte cap, known fields only, one document, no anchors.

```yaml
description: Orders stuck in pending for more than a day   # optional, one line
question:
  query: stuck_orders          # a saved query id (queries/<id>.sql) ...
  params: {days: 1}            # ... bound through queries.Bind, never as SQL text
  # or
  metric: revenue              # a metric, run at its grain total: SELECT metric('revenue')
  connection: warehouse        # optional for metric; a saved query uses its own
when:
  rows: any                    # any | none
  # or one of
  above: 1000                  # value > 1000
  below: 0.95                  # value < 0.95
  outside: [10, 90]            # value < 10 or value > 90
```

- **Exactly one question:** `query` or `metric`. Exactly one condition.
- **Threshold values** are plain decimals. A threshold condition needs a result with **one row** and **one numeric column** (or a column named `value`). Any other shape is an `error` outcome, not an alert, and the message names the shape it got.
- **`rows: any|none`** counts the result's rows. A check reads at most 1,001 rows. The alert lists the first 20 rows' first columns, capped in bytes, and says "1,000+" when the result reaches the cap.
- **Unknown fields are refused.** The validator lives with the model.

## Running a check

- **One entry point:** `workspace.CheckWatches(ctx, ws, names, runner, now)`. `runner` is the `ReadOnlyRunner` that `RunAssertions` already takes (`internal/workspace/semantics_run.go`), the agent policy's read-only path in the tightest tier.
  - A saved query is bound with `queries.Bind`, and a metric is expanded through the one expander.
  - Watch files may come from a cloned repository, so the policy, not the file, decides what runs.
  - Each statement is bounded by `CheckStatementTimeout` and can be cancelled.
- **Outcome per watch:**
  - `ok`: the condition does not hold.
  - `alert`: the condition holds. The outcome carries the value or the row count and a one-line reason, such as "revenue is 1,240, above 1,000".
  - `error`: the watch did not run or its result has the wrong shape. A connection that cannot connect is an `error` naming the connection, never an alert.
- **Partial failure:** one watch failing never stops the others.

## History: `.slaw-local/watches/history.jsonl`

- **One line per check:** watch name, file revision, time, outcome, value or row count, reason, duration. Result rows are never written, only the count and the reason. A watch over a `rows: any` question can name customers, so its sample rows stay in memory for the report.
- **Capped and pruned:** the newest 200 entries per watch, and at most 5 MB, oldest dropped first. Written append-and-compact under a file lock.
- **"Last fired" and "last checked"** in Studio and `slaw watch list` come from here.

## Surfaces

- **CLI:** `slaw watch list | show <name> | check [name…] [--json] | history [name] [--json]`.
  - `check` exits 0 when every watch is `ok`, 1 when any fired, and 2 when any is an `error` and none fired.
  - The CLI only mirrors Studio. There is no agent-only command.
- **Studio:**
  - A **Watches** view lists each watch with its question, condition, last outcome and last fired time.
  - **Check all** and per-watch **Check** buttons, with Stop, through `CancelRun`.
  - An alert panel lists each fired watch's reason and sample rows.
  - The nav badge counts watches whose latest check fired.
  - **New watch** writes a file from a small form: pick a saved query or a metric, a condition and a number, then preview the YAML. Editing is the same form. The file is the source of truth, and the watcher picks up outside changes (`KindWatch`).
- **Agent:**
  - Read-only `list_watches` and `get_watch` tools, plus `write_watch`, which validates the file and needs approval like any write to the workspace.
  - No tool or MCP method runs a check: checking is the person's action, like assertions.
  - Watches are a later step, not phase 0's first PR.
- **Bundle:** `watches/<name>.yaml` entries, validated before any write. History is never bundled.
- **Docs:** `docs/reference/watches.md`, the CLAUDE.md architecture tree and `internal/docsref`.

## Steps

1. **W1, model, store and checks, with the CLI:**
   - `internal/watches`: the model, strict decode, validation, `Store` (FileStore and Mock, one Rev per file) and the history store;
   - `workspace.CheckWatches`;
   - the `slaw watch` commands;
   - bundle entries;
   - the reference doc.
2. **W2, Studio:**
   - the Watches view, the check and cancel bindings, the alert panel and the nav badge;
   - the New watch form and the watcher kind.
3. **W3, agent:** `list_watches`, `get_watch` and `write_watch` as `ToolDef`s, plus a line in the `task-notebook-cells` or `semantic-layer` skill.

**Phase 0 exit:**
- A threshold crossing on a local DuckDB file alerts from both `slaw watch check` and Studio's Check, and shows in history.
- A watch over a hostile cloned query is refused by the policy.
- A test proves history never reaches the folder or a bundle.
