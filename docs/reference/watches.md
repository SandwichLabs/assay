# Watches: `watches/`

A **watch** is a standing question with a condition: "are there orders stuck in pending?", "is revenue above 1,000?". Checking a watch runs the question once and says whether the condition holds. If it does, the watch has **fired** and the check reports an **alert**.

A watch is a file you can write by hand, in git beside the queries and metrics it asks. Nothing here needs AI.

```
watches/
├── stuck_orders.yaml
└── revenue_cap.yaml
```

The folder is created by whatever writes the first file; `slaw workspace init` does not make an empty one.

> **What exists today.** A watch is checked only when you ask: `slaw watch check`. There is no scheduler, no background service, no notification and no delivery; a fired watch is reported where the check ran, and recorded in a local [history](#history). Studio's Watches view and agent tools for watches are later steps of the same plan ([`docs/plans/watches.md`](../plans/watches.md)). Because nothing runs by itself, a cloned workspace's watches need no enabling step yet; that gate arrives with scheduling, when a watch first runs without a person asking.

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
- **Thresholds are plain decimals** with at most **15 significant digits** in all: `0`, `-5`, `99.5`, `0.00000000000001`. A quoted `"100"`, `1e3`, `0x10`, `+5`, `.5`, `.inf`, `.nan` and a 16-digit number are refused, not coerced.
- At most **500** files: the first 500 (by name) are listed, followed by one entry named `(truncated)` that says how many were left out.
- A file that fails any of this is **not hidden**. `slaw watch list` shows it with its error (a reserved name such as `nul.yaml` says why), `slaw watch check` reports it as an `error`, and Slaw never overwrites it unless you choose to. A symbolic link is listed as "is a symbolic link; it is not read".

## How a check runs

`slaw watch check [name...]` (all watches with no name) checks each watch once, one at a time.

- The question goes through the **agent policy's read-only path in the tightest tier**, whatever the sandbox setting is. A watch file or saved query from a cloned repository cannot do more than read: a `COPY ... TO`, a `read_text` of a file outside `data/`, a `DROP` or an `ATTACH` is refused by the policy (see [agent-policy.md](agent-policy.md)), and the watch is an `error` that says `refused: a watch may only read (no writes, COPY, ATTACH or SET; files only inside data/)`.
- A **saved query** is bound with the same binder as `slaw query --saved` and the agent's `run_saved_query`: the statement is the query's own text, and a `params` value is typed by the query's declared parameters and travels as a bound parameter. A value that is not valid for the declared type (`days: lots` for an integer) is an `error` before anything runs. A declared parameter you do not give takes its default; a required one with no default is an `error` telling you to add `question.params.<name>`. A saved query on a connection the workspace does not define is an `error` that says so (`slaw conn list`).
- A **metric** runs as `SELECT * FROM metric('<name>')` through the one metric expander, the same statement as the Semantics view's "Try it" with no dimensions.
- Each statement is bounded by a **2 minute** timeout and by Ctrl-C. A threshold check reads at most **5** rows; a `rows` check reads at most **1,001**.

### The value a threshold reads

`above`, `below` and `outside` need a result with **exactly one row and one numeric column** (or, with several columns, exactly one named `value`, in any case). Any other shape is an `error`, never an alert, and the message names the shape it got: no rows, N rows, N columns, two columns named `value`, a text column, a `NULL`, a value that is not finite.

Integer and decimal values (`BIGINT`, `HUGEINT`, `DECIMAL`) are compared with the threshold exactly, however large; a `DOUBLE` is compared as a double. A reason never shows a non-zero number as `0`: a value too small for six decimals is written in full.

### Row counts

`rows: any` and `rows: none` count the rows. The count stops at 1,001: a result that reaches it is reported as "1,000+ rows". In `--json` and the history that is `rows: 1001` with `rows_capped: true`, a lower bound, not the count. An alerting `rows` check shows the **first 20 rows' first 4 columns** in the report, each cell at most 80 characters and the whole sample at most 4 KiB, with control characters shown as `<U+XXXX>`. A sample exists in the report only; it is never written to the history, because a result can name customers.

### Outcomes

| Outcome | Meaning |
|---|---|
| `ok` | The condition does not hold. |
| `alert` | The condition holds. The reason says why: `revenue is 1,240, above 1,000`, `stuck_orders returned 3 rows`. |
| `error` | The watch did not run, or its result had the wrong shape. A connection that cannot connect is an `error` naming the connection (`could not connect to warehouse: ...`), never an alert. Also: a file that is not valid (`the file is not valid: ...`), a saved query or metric that does not exist, a statement the policy refuses, a timeout, a cancelled check. |

One watch failing never stops the others. Ctrl-C (or a cancel in Studio) stops the check in flight: the results so far are printed, the interrupted watch is reported as an `error` and is not recorded, since it says nothing about the watch, and the command exits 2. Reasons are one line of at most 300 characters, with credential-shaped text redacted.

A database error can quote a value of the row it choked on (`Could not convert string 'alice@example.com' to INT32`). The **report** that ran the check shows the database's message, without the wrapper chain and the `LINE n:` excerpt of the statement; the **history** keeps only its error class (`could not run: Conversion Error`), and for a connection failure only `could not connect to warehouse`. An alert's or an ok's reason is only our own formatting of the value or the row count.

## History

Every check is appended to **`.slaw-local/watches/history.jsonl`**: one JSON line with the watch name, the file's revision, the time, the outcome, the value or row count, the reason and the duration. It never holds result rows, and never a value a database error quoted: an error outcome's reason is stored as its class only (see above).

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

## Sharing

A watch file travels in git like any other file. In a `.slaw` bundle it is an entry `watches/<name>.yaml` of kind `watch`: `slaw export bundle --watches NAME` (with no selection flags everything is exported, watches included). The file is decoded with the store's strict decoder while the bundle opens, so one bad watch refuses the whole bundle before anything is written. Import writes the file's own bytes; a name that exists is renamed (`stuck_orders_2`), replaced against the revision just read, or skipped, and a watch whose saved query or metric the same bundle renamed is pointed at the new name; one whose query or metric was skipped because the workspace already has its own is told so in the import's notes. **The history is never bundled**: no path under `.slaw-local/` is accepted as an entry. See [workspaces.md](workspaces.md#sharing-without-git-slaw-bundles).

## Who can do what

Checking a watch is **your action**. No agent tool and no MCP method checks a watch, as with [assertions](semantics.md). The watcher reports a file change under `watches/` as a `watch` event, so an editor or `git pull` shows up in a running Studio.
