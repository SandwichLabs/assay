# Watches: `watches/`

A **watch** is a standing question with a condition: "are there orders stuck in pending?", "is revenue above 1,000?". Checking a watch runs the question once and says whether the condition holds. If it does, the watch has **fired** and the check reports an **alert**.

A watch is a file you can write by hand, in git beside the queries and metrics it asks. Nothing here needs AI.

```
watches/
├── stuck_orders.yaml
└── revenue_cap.yaml
```

The folder is created by whatever writes the first file; `slaw workspace init` does not make an empty one.

> **What exists today.** A watch is checked only when you ask: `slaw watch check`, or Check in Studio's [Watches view](#studio). There is no scheduler, no background service, no notification and no delivery; a fired watch is reported where the check ran, and recorded in a local [history](#history). An agent can list, read and write watch files but cannot check one ([Agent](#agent)); the plan is [`docs/plans/watches.md`](../plans/watches.md). Because nothing runs by itself, a cloned workspace's watches need no enabling step yet; that gate arrives with scheduling, when a watch first runs without a person asking.

## The file: `watches/<name>.yaml`

```yaml
version: 1                                                 # optional; absent means 1
description: Orders stuck in pending for more than a day   # optional, one line
question:
  query: stuck_orders        # a saved query id (queries/<id>.sql) ...
  params: {days: 1}          # ... its parameters, bound by name, never as SQL text
  # or
  metric: revenue            # a metric, run at its grain total
when:
  rows: any                  # any | none
  # or exactly one of
  above: 1000                # value > 1000
  below: 0.95                # value < 0.95
  outside: [10, 90]          # value < 10 or value > 90
```

| Field | Meaning |
|---|---|
| `version` | Optional. `1` is the only version; absent means 1. Any other value is refused with "needs a newer Slaw". |
| `description` | Optional. One line, at most 200 characters. |
| `question` | What to ask. Exactly one of `query` or `metric`. |
| `question.query` | A saved query id. The query runs on its own connection, which must be one the workspace defines. |
| `question.params` | Values for the saved query's declared parameters, written as you would give them to `slaw query --saved -p name=value` (`days: 1`, `region: east`). At most 20, each at most 200 characters, one line. Only with `query`. |
| `question.metric` | A metric name from `semantics/metrics/`. It is run at its grain total (no `by` dimensions), on the connection of the metric's saved query. There is no connection field: the metric expander refuses any other connection. |
| `when` | The condition. Exactly one of the four below. |
| `when.rows` | `any` fires when the result has rows; `none` fires when it has none. |
| `when.above` | Fires when the value is greater than the number. |
| `when.below` | Fires when the value is less than the number. |
| `when.outside` | Two numbers, `[low, high]`, with low less than high. Fires when the value is below low or above high. |

The file is read as strictly as a [semantics file](semantics.md#rules-every-yaml-file-shares), by the same decoder (`internal/strictyaml`):

- At most **16 KiB** and **500** YAML nodes, checked before anything is parsed.
- No control characters, no anchors or aliases, no explicit tags, no merge keys, one document, no unknown keys, no duplicate keys. Nothing is interpolated: `${ENV}` is just text.
- The **name** is the file name without `.yaml` and follows the connection-name rule: `^[a-z_][a-z0-9_]{0,62}$`. A Windows device name (`con`, `nul`) is refused.
- **Thresholds are plain decimals**, at most **40 characters**: `0`, `-5`, `99.5`, `100000000000000000000`. A quoted `"100"`, `1e3`, `0x10`, `+5`, `.5`, `.inf` and `.nan` are refused, not coerced.
- At most **500** files: the first 500 (by name) are listed, followed by one entry named `(truncated)` that says how many were left out.
- A file that fails any of this is **not hidden**. `slaw watch list` shows it with its error (a reserved name such as `nul.yaml` says why), `slaw watch check` reports it as an `error`, and Slaw never overwrites it unless you choose to: a write over it needs the revision of the very bytes that were shown (or an explicit overwrite), so a file that changed meanwhile is a conflict. A symbolic link is listed as "is a symbolic link; it is not read".

## How a check runs

`slaw watch check [name...]` (all watches with no name) checks each watch once, one at a time.

- The question goes through the **agent policy's read-only path in the tightest tier**, whatever the sandbox setting is. A watch file or saved query from a cloned repository cannot do more than read: a `COPY ... TO`, a `read_text` of a file outside `data/`, a `DROP` or an `ATTACH` is refused by the policy (see [agent-policy.md](agent-policy.md)), and the watch is an `error` that says `refused: a watch may only read (no writes, COPY, ATTACH or SET; files only inside data/)`. A statement that merely fails to bind (an unknown column, a typo) is not a refusal: it is an `error` showing the database's message (`could not run: Binder Error: ...`).
- A **saved query** is bound with the same binder as `slaw query --saved` and the agent's `run_saved_query`: the statement is the query's own text, and a `params` value is typed by the query's declared parameters and travels as a bound parameter. A value that is not valid for the declared type (`days: lots` for an integer) is an `error` before anything runs. A declared parameter you do not give takes its default; a required one with no default is an `error` telling you to add `question.params.<name>`. A saved query on a connection the workspace does not define is an `error` that says so (`slaw conn list`).
- A **metric** runs as `SELECT * FROM metric('<name>')` through the one metric expander, the same statement as the Semantics view's "Try it" with no dimensions.
- **While Studio (or `slaw mcp serve`) has the workspace open** it owns the local DuckDB files, so a check here could only report that it cannot connect. `slaw watch check` therefore refuses first, before anything runs and with nothing recorded: `Slaw Studio (pid 4242) has this workspace open; check its watches from there. Nothing was checked or recorded.` The exit status is 2. Check from Studio instead, or close it.
- Each statement is bounded by a **2 minute** timeout and by Ctrl-C. A threshold check reads at most **5** rows; a `rows` check reads at most **1,001**.

### The value a threshold reads

`above`, `below` and `outside` need a result with **exactly one row and one numeric column** (or, with several columns, exactly one named `value`, in any case). Any other shape is an `error`, never an alert, and the message names the shape it got: no rows, N rows, N columns, two columns named `value`, a text column, a `NULL`, a value that is not finite.

Integer and decimal values (every integer width, `HUGEINT`, `DECIMAL`) are compared with the threshold exactly, however large. A `FLOAT` or `DOUBLE` is compared as a double, with the threshold read as the nearest double, so a `DOUBLE` 0.95 is not below the threshold 0.95. A reason never shows a non-zero number as `0`: a value too small for six decimals is written in full.

### Row counts

`rows: any` and `rows: none` count the rows. The count stops at 1,001: a result that reaches it is reported as "1,000+ rows". In `--json` and the history that is `rows: 1001` with `rows_capped: true`, a lower bound, not the count. An alerting `rows` check shows the **first 20 rows' first 4 columns** in the report, each cell at most 80 characters and the whole sample at most 4 KiB, with control characters shown as `<U+XXXX>`. A sample exists in the report only; it is never written to the history, because a result can name customers.

### Outcomes

| Outcome | Meaning |
|---|---|
| `ok` | The condition does not hold. |
| `alert` | The condition holds. The reason says why: `revenue is 1,240, above 1,000`, `stuck_orders returned 3 rows`. |
| `error` | The watch did not run, or its result had the wrong shape. A connection that cannot connect is an `error` naming the connection (`could not connect to warehouse: ...`), never an alert. Also: a file that is not valid (`the file is not valid: ...`), a saved query or metric that does not exist, a statement the policy refuses, a timeout, a cancelled check. |

One watch failing never stops the others. Ctrl-C (or a cancel in Studio) stops the check in flight: the results so far are printed, the interrupted watch is reported as an `error` and is not recorded, since it says nothing about the watch, and the command exits 2, even if an alert had already fired. After the first Ctrl-C a second one quits at once. Reasons are one line of at most 300 characters, with credential-shaped text redacted.

A database error can quote a value of the row it choked on (`Could not convert string 'alice@example.com' to INT32`). The **report** that ran the check shows the database's message, without the wrapper chain and the `LINE n:` excerpt of the statement; the **history** keeps only its error class (`could not run: Conversion Error`), and for a connection failure only `could not connect to warehouse`. An alert's or an ok's reason is only our own formatting of the value or the row count.

## History

Every check is appended to **`.slaw-local/watches/history.jsonl`**: one JSON line with the watch name, the file's revision, the time, the outcome, the value or row count, the reason and the duration. It never holds result rows, and never a value a database error quoted: an error outcome's reason is stored as its class only (see above). The `value` is a JSON number kept exactly: an integer or decimal in full however large, a double in its shortest form.

- The newest **200** entries of each watch are kept, and the file is at most **5 MB**; the oldest are dropped first.
- Appends and the pruning run under a cross-process file lock (`.slaw-local/watches/history.lock`), so Studio and a CLI check cannot interleave.
- The folder is `0700` and the file `0600`. A symbolic link anywhere on the path, or a file that is not a regular file, is refused rather than followed.
- It is machine-local and git-ignored with the rest of `.slaw-local/`. It is never committed and never goes in a `.slaw` bundle.

`slaw watch list` takes "last checked" and "last fired" from it. A history that cannot be read is reported as a warning and does not hide the list.

## Commands

| Command | |
|---|---|
| `slaw watch list [--json]` | Every file under `watches/`: name, question, condition, last checked, last fired and, for a file that cannot be read, why. Reads files and the history; no connection is opened and nothing runs. |
| `slaw watch show <name>` | The file as stored (comments kept; control characters shown as `<U+XXXX>`). A file that fails validation is still printed, with the reason and exit status 2. |
| `slaw watch check [name...] [--json]` | Check now. Prints a table of outcomes and reasons, then the sample of each alerting `rows` check. |
| `slaw watch history [name] [--json] [-n N]` | Past checks, newest first (the newest 20; `-n 0` is all). |

All take `--workspace`. Names and messages are printed with control characters as `<U+XXXX>`.

**Exit status of `slaw watch check`:** `0` every watch is `ok`; `1` at least one alert fired; `2` none fired but some watch could not be checked, and for a usage or open error (an unknown watch name, a bad flag, no workspace). These are the statuses of `slaw semantics check`, so `1` always means "a condition holds". The other `slaw watch` commands use `2` for their errors for the same reason.

```
$ slaw watch check
STATUS  WATCH         REASON
alert   revenue_cap   revenue is 35, above 30
error   exfil         refused: a watch may only read (no writes, COPY, ATTACH or SET; files only inside data/)
ok      volume        order_count is 4; alerts above 10
alert   stuck_orders  stuck_orders returned 2 rows

1 ok, 2 alerts, 1 error

stuck_orders: first 2 rows (id, customer)
  1 | acme
  2 | globex
```

## Studio

The **Watches** view (nav entry beside Semantics) is the same surface as `slaw watch`, drawn for the desktop app. It adds no rule of its own: the file format is the decoder's, a check is `workspace.CheckWatches`, and a save is the store's compare-and-write.

- **The list.** Every file under `watches/`, with its question, its condition, the outcome and reason of its last check, when it was last checked and when it last fired. The last outcome is the newest check: the one you ran in this window, unless the [history](#history) holds a newer one (a check run by the command line), so a row, its alert card and the badge always agree. The sample rows appear only when this window's result is the newest. A file that cannot be read is listed with its error and the revision of the bytes that could not be read, and cannot be checked. Edit opens an empty form over it, and saving replaces it only if the file is still exactly that revision (a file that changed since is the conflict banner, never a silent overwrite). Delete is the same compare-and-delete.
- **Check, Check all and Stop.** Each row has a Check button and the header has Check all. A check runs one watch at a time through the same read-only path as `slaw watch check`, and every outcome is recorded in the history. While it runs, each row says `queued`, `checking…` or the status it finished with (Studio sends only the watch's name and status as it goes, never rows), and the result of a run is announced once as a sentence ("Checked 9 watches: 3 fired, 4 errors."). **Stop** ends the run: the watches checked before it keep their results, and the one that was running shows as `stopped` in this window only. It was cut short, so it says nothing about the watch and is never recorded in the history (the check marks such a result `interrupted`; `slaw watch check --json` carries the same flag and its table says `stopped`). Leaving the view also stops a check in flight.
- **Alerts.** Every watch whose newest check fired is listed with its reason. For a `rows` check run in this window the panel also shows the first rows it read as a small table. Those rows exist only in the result of that check in this window; they are never written to the history, so after a restart the panel asks you to check again to see them.
- **Errors.** A watch that could not run shows `error` and its reason in its row: the database's message for a check run in this window, the error class alone when read back from the history.
- **History.** A row's History button opens its recorded checks, newest first (the newest 50 are shown): when, outcome, what it read, reason and duration.
- **The badge.** The nav entry shows how many watches' newest check fired. It is read from the history, so it is the same on every start, and it is updated when a check finishes, when a watch is saved or deleted, and when a watch file changes on disk.
- **New watch and Edit.** A form with a name (the file name; fixed once saved), an optional description, a saved query (with an input for each parameter it declares) or a metric, and a condition: any rows, no rows, above, below, or outside two numbers. The form shows the file it will write, drawn by the same encoder a save uses, and every refusal (a threshold that is not a plain decimal, a bad name) is the decoder's. Saving is against the revision the form loaded:
  - a name that already exists is refused with a message, not replaced;
  - a file that changed on disk while you edited it (an editor, `git pull`) shows the conflict banner with **Reload from disk** and **Keep mine**; a file deleted meanwhile offers to close the form.
- **Missing questions.** A watch whose saved query or metric no longer exists (deleted, renamed, or a metric file that cannot be read) opens in the form with that name marked `(missing)` and a note; Save stays off until another is picked, so the old name is never written back by accident. A saved query's parameter that has no default must be given a value before Save.
- **Delete** removes the file, against the revision on screen, and is refused without one. Its history stays on this computer.
- **Outside edits.** The folder watcher reports a change under `watches/` as a `watch` event, so a file saved by an editor or arriving with `git pull` appears in a running Studio, and the badge follows. It also reports an append to the check history (`.slaw-local/watches/history.jsonl`) as a `watch_history` event, so a check run by `slaw watch check` in a terminal updates a running Studio's list and badge. That event carries no content; Studio reads the history again.
- **Export.** The Export dialog lists the workspace's watches (all ticked, like the other content) and writes them to the bundle. The history is not offered, because it is never bundled.

Nothing in Studio can check, save or delete a watch for an agent: these are the person's actions, and no agent tool or MCP method reaches them.

## Agent

Three tools, the same for the in-app agent and for MCP clients (`docs/reference/mcp.md`):

| Tool | Tier | What it does |
|---|---|---|
| `list_watches` | every tier (introspect) | Each watch: `name`, `description`, `question`, `when`, `condition`, `rev`, and for a file that cannot be read its `error`. If the watch has been checked, `last_checked` and `last_status` (`ok`, `alert` or `error`) from the [history](#history). |
| `get_watch` | every tier (introspect) | One watch's fields, its canonical YAML (drawn by the same encoder a save uses) and its `rev`. |
| `write_watch` | `read-write` and `full` (content) | Creates or replaces `watches/<name>.yaml`. |

- **Reads show a status, never results.** The agent is handed the history through a read-only view (no way to append). `list_watches` reports when a watch was last checked and its status, and nothing else: no reason, no value, no row count, no duration, no sample rows (the history holds none).
- **`write_watch`** takes `name`, `question` (`{query, params?}` or `{metric}`), `when` (`{rows}`, `{above}`, `{below}` or `{outside: [low, high]}`), an optional `description` and an optional `rev`.
  - It is validated by the decoder and encoder that Studio's New watch form and `slaw watch` use, so a bad name, a threshold that is not a plain decimal, two conditions and the rest are refused with the decoder's words. Thresholds may be sent as numbers or as text; text keeps a large or exact number as written.
  - The saved query or metric must exist (a missing one is refused: `save_query` first, or a metric is proposed with `propose_definition`), and `params` must name parameters the query declares. A required parameter with no default and no value is a **warning** in the reply, not a refusal.
  - Without `rev` it only creates, and an existing name is refused. With the `rev` from `get_watch` (or `list_watches`, for an unreadable file) it replaces the file; a stale `rev` is a conflict and changes nothing.
  - The reply carries the canonical `file`, the new `rev`, any `warnings`, and a note that the watch is written and not checked. A refusal that has a canonical text (everything past the decoder, such as a missing query) returns it in the error's data.
- **Gating is a content tool's.** `read-only` refuses it; "Ask before changes" shows the approval card ("Save watch NAME: query stuck_orders, fires when any rows"); headless MCP with the setting on validates first, then answers "valid and ready, not saved" with the canonical `file` and `warnings` and nothing written, as `write_artifact` does. See [agent-policy.md](agent-policy.md).
- **No tool checks a watch.** There is no tool or MCP method that runs a check, reads a reason or sees result rows. Tell the person to check in Studio's Watches view or with `slaw watch check`. A test fails if a tool is added that offers to.

## Sharing

A watch file travels in git like any other file. In a `.slaw` bundle it is an entry `watches/<name>.yaml` of kind `watch`: `slaw export bundle --watches NAME` (with no selection flags everything is exported, watches included). The file is decoded with the store's strict decoder while the bundle opens, so one bad watch refuses the whole bundle before anything is written. Import writes the file's own bytes; a name that exists is renamed (`stuck_orders_2`), replaced against the revision just read, or skipped, and a watch whose saved query or metric the same bundle renamed is pointed at the new name; one whose query or metric was skipped because the workspace already has its own is told so in the import's notes. **The history is never bundled**: no path under `.slaw-local/` is accepted as an entry. See [workspaces.md](workspaces.md#sharing-without-git-slaw-bundles).

## Who can do what

Checking a watch is **your action**. No agent tool and no MCP method checks a watch, as with [assertions](semantics.md); an agent can [write the file](#agent) and you check it. The watcher reports a file change under `watches/` as a `watch` event, so an editor or `git pull` shows up in a running Studio.
