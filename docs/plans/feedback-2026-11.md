# Feedback batch 2026-11-04: #320, #321, #322

Triage of the three issues filed after the v0.6.0 workshop session. A Fable judge checked each claim against main (625d292), and the main claims were spot-checked again by hand. This plan turns the verdicts into five work packages. Every package is built in the usual loop: a Sonnet implementer, an Opus adversarial review, live Rodney QA where Studio changes, then a merge after APPROVE and green CI.

## Verdicts

| Item | Finding | Verdict |
|---|---|---|
| #322 `_` in artifact names | `useArtifact.ts:14` leaves out `_`, while `filestore.ValidID`, `newItem.ts:9` and the docs allow it | Fix now (WP1) |
| #322 skills and theme patterns | Skills match `skills.NamePattern`, which is kebab-case by design. Themes have no Go-side rule. | No change |
| §1 ask-before-changes gates `propose_definition` | `handler.go:388` gates every write-category tool. The inbox's accept step approves the same change a second time. | Exempt it (WP3, decided D1) |
| §1 narrower approvals ("next N writes") | Would add a fourth switch | Fold into #25 (elicitation approver) |
| §2 tier vs ask confusion | The refusal text names the tier but doesn't say which setting blocks the write | Reword now (WP1). Studio hint in WP3. |
| §3a `slaw mcp call` | An agent-only CLI | Won't do (D10) |
| §3b `semantics propose`, `query save` | `propose` was already declined in #302 | Won't do / backlog |
| §3c `accept --all` | `accept` takes exactly one id, and Studio has no bulk accept either | WP4, built for the CLI and Studio together |
| §4 glossary revs | `proposals/accept.go:146` says terms are judged per term, but it compares the file rev, and so does `handler_semantics.go:406` | Bug (WP2) |
| §5 MCP blocked while Studio holds the hub | Correct (`hub/serve.go:233`) | Fix the cause: #96 (WP4). Revisit a read-only fallback only if #96 isn't enough. |
| §6a notebook check | — | Fold into #308 |
| §6b artifact check | — | Fold into #245 (E5 slice 2) |
| §6c `artifact render` for static pages | `artifacts.RenderHTMLPage` already exists. #302 declined it under direction §7, which is about A2UI. | Add for static pages (WP3, decided D2) |
| §6d dry run drops `queries[].columns` | `validateOnly` throws away the vet result (`handler.go:491`) | Fix now (WP1) |
| §7a annotations | = #320 | WP5 |
| §7b multi-series `lineChart` | = #13 item 2 | Fold |
| §7c pages can't read the theme mode | `pagehost_files.go:178` consumes `--slaw-theme-type` and emits only `color-scheme` | Fix now: emit the property as well (WP1). Studio doesn't rewrite the page's `<html>`. |
| §7d role names missing from `artifacts.md` | They are listed in `themes.md` | Cross-link (WP1) |
| §8a `read_only: true` not written | By design, since `IsReadOnly` defaults to true. The committed file just isn't explicit. | Write it when the flag is given (WP1) |
| §8b composite `id_columns` | — | Fold into #287 (needs design) |
| §8c `list_queries` shape | The reporter is mistaken: every `list_*` returns `{plural: [...]}`. Only the line at `mcp.md:108` is misleading. | Fix the doc (WP1) |
| §8d `as_of` data window | — | Fold into #287 (low priority) |
| #320 annotations | Correct: the schema is closed and has no marker field | WP5, after #13 |

## WP1: agent-experience fixes (v0.6.1, size S)

1. **#322:** `useArtifact.ts` imports the name rule from `lib/newItem.ts`. Export `NAME_RE`/`RESERVED_RE`, or a `validName()` helper, and delete the local regex. Add an underscore case to `views/ArtifactView.spec.ts`. Add a Go test that keeps the TS pattern in step with `filestore.idRE` (read the TS file, the same way `docsref` does).
2. **§6d:** carry `artifactVetResult` (each query's columns) through `validateOnly` and the dry run into the approval refusal's `Error.Data`, for both `write_artifact` and `save_query`. Files: `internal/agents/handler.go`, `handler_artifacts.go`, `sandbox.go`, `approval_text.go`. Update the shapes in `docs/reference/mcp.md`.
3. **§2:** reword `contentNeedsApproval`/`dataNeedsApproval` in `approval_text.go` to name both settings and point at the one that blocks, e.g. "Sandbox: full (allows this). Ask before changes: on (this is what stopped it)". Update the help and agent-policy text that quotes the refusal.
4. **§7c/§7d:** `themeStylesheet` also emits `--slaw-theme-type:light|dark`, so a page stylesheet that sets `color-scheme` can still read the mode. Add a test in `studio/pagehost_files_test.go`. In `docs/reference/artifacts.md`, document the property and link the role list in `themes.md`.
5. **§8a:** `cmd/conn.go` writes `read_only: true` when `--read-only` is set explicitly (MotherDuck excluded), with a test.
6. **§8c:** fix `docs/reference/mcp.md:108`.

Release v0.6.1 after WP1 and WP2.

## WP2: per-term glossary revs (S/M, P2)

- Add `semantics.GlossaryTermRev(text, term)` in `internal/semantics/glossary.go`, a short hash of the term's section text.
- `get_definition` with kind glossary and a term returns the term's rev. Without a term, it still returns the file rev.
- `checkGlossaryTarget` (`handler_semantics.go:406`) and `Preview.markStale` (`proposals/accept.go:153`) compare against the term rev. Accept stays compare-and-write on the file: re-read the file, check the term rev, splice, then write against the file rev just read. An edit to another term then no longer makes the proposal stale.
- Proposals already filed with the file rev: accept treats a `replaces_rev` equal to the current file rev as valid too, so the inbox doesn't break on upgrade.
- `slaw semantics show glossary <term>` prints the term rev. Update `docs/reference/semantics.md`.
- Tests: two proposals filed together for different terms, both accepted; a proposal for a term someone else changed is reported stale.

## WP3: proposals under ask-before-changes, and `artifact render` (S/M)

- `handler.go:388` skips `awaitApproval` for `propose_definition` when `nonLocal` is empty. The tier gate still applies, and the inbox caps (`proposals/model.go:58`: 200 open, 1000 files, 64 KB each) still bound it.
- Update the docs and the approval tool table: `agent-policy.md:26`, `semantics.md`, `cmd/help.go:231`, and the `semantic-layer` built-in skill.
- Studio, in `AgentSettings`: an inline hint when the tier is raised while "Ask before changes" is still on.
- Tests: with ask on, a headless `propose_definition` files and `write_cell` is refused. The read-only tier still refuses a proposal.
- **`slaw artifact render <name> [--out dir] [-p name=value]`** (D2), for static `view: html` pages only:
  - It refuses scripted pages, A2UI views and script views.
  - It requires the page to be allowed (same path and `PageHash` decision as Studio), so a pulled page isn't rendered unseen.
  - It renders with `artifacts.RenderHTMLPage` through the person's read-only, policy-gated query path (`hub.OpenOptions.PolicyGatedRuns`, as `slaw semantics check` does). It writes `index.html`, `_slaw/theme.css` and the page's static files to `--out`.
  - It mirrors Studio's page frame, so it fits D10. Mention it in `docs/reference/artifacts.md` and the CLI reference. Tests cover a static page, a refused scripted page and a not-allowed page.

## WP4: #96 and bulk accept (M, next release)

- **#96:** remember the MCP server setting per workspace in machine-local prefs and restart the server when the workspace opens. This removes the §5 blocker, since `slaw mcp serve` attaches to Studio's server. Follow the existing #96 scope.
- **Bulk accept:**
  - `slaw semantics accept --all [--kind K]` prints each diff and asks once, or skips the prompt with `--yes` (people only, needs a TTY, like `repl`). It stops at the first stale proposal and reports it.
  - Studio's Semantics inbox gets "Accept all" with a confirmation that lists the proposals. Both call one `proposals.AcceptMany` loop that does compare-and-write on each.

## WP5: charts track (M/L, backlog)

Order: #13 item 1 (a real time axis and date ticks), then #13 item 2 (multi-series `lineChart`), then #320.

#320 scope:
- `annotations` on `lineChart`, `areaChart` and `columnChart`, with `at`, `label` and `kind` (solid or dashed), and `color` from the closed enum (default `muted`).
- Inline `rows`, or `sql`+`connection`. In artifact views, a manifest `query`.
- At most 50 annotations; over the cap is a validation note.
- Leave out `barChart`: its bars are horizontal, so a vertical rule has no meaning there.
- Defer `until` bands.
- Files:
  - Go: `internal/a2ui/schema.go` and `view.go`, the validator tests, and the view-fit mirror in `internal/agents/notebook_views.go`.
  - Vue: one shared overlay component, and a second live binding through `a2ui/useLiveQuery`.
  - Agent guidance: `a2ui/prompt.go` and the `task-notebook-cells` skill.
  - Docs: `docs/reference/notebooks.md` (docsref).

## Folded, not planned here

- §6a goes to #308.
- §6b goes to #245.
- §8b and §8d go to #287.
- §1's narrower approvals go to #25.
- §7b goes to #13.

Each target issue gets a comment linking back to #321.

## Decisions (owner, 2026-11-04)

- **D1 (§1):** `propose_definition` files into the inbox while "Ask before changes" is on. The tier gate and the inbox caps stay.
- **D2 (§6c):** add `slaw artifact render` for static `view: html` pages only. A2UI and notebook render stay declined.
