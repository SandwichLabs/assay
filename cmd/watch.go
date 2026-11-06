package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/slaw/internal/strictyaml"
	"github.com/SandwichLabs/slaw/internal/watches"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// Exit statuses of `slaw watch check`: 0 every watch is ok, 1 at least one
// alert fired, 2 anything that stopped the run from saying whether one would:
// a watch that could not run (and none fired), an unknown name, a bad flag, no
// workspace. They are the statuses of `slaw semantics check` (checkExitFail is
// "the data says so", checkExitError is "fix the command or the setup"), so
// 1 only ever means an alert. The other watch commands use 2 for their errors
// for the same reason.

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "List, show and check the workspace's watches",
	Long: `A watch is a standing question with a condition, kept as watches/<name>.yaml:
a saved query (with parameter values) or a metric, and one of "any rows",
"no rows" or a threshold (above, below, outside). Checking a watch runs the
question once through the agent policy's read-only path and reports ok, alert
or error; every check is also logged in .slaw-local/watches/history.jsonl,
which stays on this computer (it holds the outcome, never the rows).

Nothing checks a watch by itself: "check" is your action. "list" and "show"
only read files and the history; "history" prints past checks. See ` + docRef("watches"),
}

var watchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the watches with their last check and last alert",
	Long: `List every file under watches/: name, question, condition, the last time it was
checked and the last time it fired (from the local history) and, for a file
that cannot be read, why. The list is complete: a broken file is shown with
its error, never hidden. No connection is opened and no query is run.`,
	SilenceUsage: true,
}

var watchShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print one watch's file as stored",
	Long: `Print one watch file as it is stored (comments and layout kept; control
characters are shown as <U+XXXX>). A file that fails validation is still
printed, with the reason in the error and a non-zero exit, so it can be read
and fixed.`,
	SilenceUsage: true,
}

var watchCheckCmd = &cobra.Command{
	Use:   "check [name...]",
	Short: "Check watches now and report which ones fired",
	Long: `Run each named watch (all of them with no name) once and report ok, alert or
error. The question runs through the agent policy's read-only path whatever
the sandbox setting, so a watch file or saved query from a cloned repository
cannot do more than read; values travel as bound parameters. A threshold
check wants one row and one numeric column (or a column named value); any other
shape is an error, not an alert. A watch whose connection cannot connect is an
error naming the connection. One watch failing never stops the others. An
alerting "rows" check lists its first rows (they are shown here and never
written to the history).

Exit status: 0 when every watch is ok, 1 when any alert fired, 2 when none fired
but some watch could not be checked, and 2 for a usage or open error (an
unknown watch name, a bad flag, no workspace). Names and messages printed here
are shown with control characters as <U+XXXX>. See ` + docRef("watches"),
	SilenceUsage: true,
}

var watchHistoryCmd = &cobra.Command{
	Use:   "history [name]",
	Short: "Print past checks, newest first",
	Long: `Print the checks recorded in .slaw-local/watches/history.jsonl, newest first:
when, outcome, the watch, its value or row count and the reason. The history
keeps the newest 200 checks of each watch (and at most 5 MB). It never holds
result rows and never leaves this computer.`,
	SilenceUsage: true,
}

func init() {
	runsAs2(watchListCmd, cobra.NoArgs, runWatchList)
	runsAs2(watchShowCmd, cobra.ExactArgs(1), runWatchShow)
	runsAs2(watchCheckCmd, cobra.ArbitraryArgs, runWatchCheck)
	runsAs2(watchHistoryCmd, cobra.MaximumNArgs(1), runWatchHistory)
	watchListCmd.Flags().Bool("json", false, "output as JSON")
	watchCheckCmd.Flags().Bool("json", false, "output as JSON")
	watchHistoryCmd.Flags().Bool("json", false, "output as JSON")
	watchHistoryCmd.Flags().IntP("limit", "n", 20, "how many checks to print (0 is all)")
	for _, c := range []*cobra.Command{watchListCmd, watchShowCmd, watchCheckCmd, watchHistoryCmd} {
		addWorkspaceFlag(c)
	}
	watchCmd.AddCommand(watchListCmd, watchShowCmd, watchCheckCmd, watchHistoryCmd)
	rootCmd.AddCommand(watchCmd)
}

// --- list ---------------------------------------------------------------------

// watchRow is one line of the list.
type watchRow struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Question    string     `json:"question,omitempty"`
	When        string     `json:"when,omitempty"`
	LastChecked *time.Time `json:"last_checked,omitempty"`
	LastFired   *time.Time `json:"last_fired,omitempty"`
	LastStatus  string     `json:"last_status,omitempty"`
	Rev         string     `json:"rev,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func watchRows(list []watches.Watch, entries []watches.Entry) []watchRow {
	sum := watches.Summarize(entries)
	rows := make([]watchRow, 0, len(list))
	for _, w := range list {
		r := watchRow{Name: w.Name, Description: w.Description, Rev: w.Rev, Error: w.Error}
		if w.Error == "" {
			r.Question = questionText(w.Question)
			r.When = w.When.Describe()
		}
		if s, ok := sum[w.Name]; ok {
			r.LastChecked, r.LastFired, r.LastStatus = s.LastChecked, s.LastFired, string(s.LastStatus)
		}
		rows = append(rows, r)
	}
	return rows
}

func questionText(q watches.Question) string {
	switch {
	case q.Query != "":
		return "query " + q.Query
	case q.Metric != "":
		return "metric " + q.Metric
	}
	return ""
}

func runWatchList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	ctx := cmdCtx(cmd)
	list, err := ws.Watches().List(ctx)
	if err != nil {
		return err
	}
	entries, err := ws.WatchHistory().Entries(ctx, "")
	if err != nil {
		// A history that cannot be read is not a reason to hide the watches.
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: the history could not be read: %s\n", strictyaml.EscapeHidden(foldSpace(err.Error())))
	}
	rows := watchRows(list, entries)
	out := cmd.OutOrStdout()
	if asJSON {
		return encodeJSON(out, rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "No watches. Add files under watches/ (see "+docRef("watches")+").")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tQUESTION\tWHEN\tLAST CHECKED\tLAST FIRED\tERROR")
	unreadable := 0
	for _, r := range rows {
		if r.Error != "" {
			unreadable++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", esc(r.Name), dash(esc(r.Question)), dash(esc(r.When)),
			whenText(r.LastChecked, "never"), whenText(r.LastFired, "never"), dash(esc(foldSpace(r.Error))))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if unreadable > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "%d file(s) could not be read; see the ERROR column\n", unreadable)
	}
	return nil
}

// esc is s on one line with control characters shown as <U+XXXX>.
func esc(s string) string { return strictyaml.EscapeHidden(foldSpace(s)) }

// whenText is a time as the local date and minute, or none.
func whenText(t *time.Time, none string) string {
	if t == nil {
		return none
	}
	return t.Local().Format("2006-01-02 15:04")
}

func encodeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// --- show ---------------------------------------------------------------------

func runWatchShow(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	ctx := cmdCtx(cmd)
	src, err := ws.Watches().Source(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), strictyaml.EscapeHidden(src.Text))
	if _, err := ws.Watches().Get(ctx, args[0]); err != nil {
		return fmt.Errorf("this file does not pass validation: %s", watches.ReadableError(err))
	}
	return nil
}

// --- check --------------------------------------------------------------------

func runWatchCheck(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	o, r, err := openReadOnlyRun(cmd, "slaw watch check")
	if err != nil {
		return err
	}
	defer o.Close()
	// Ctrl-C stops the check in flight; the results so far are still printed.
	ctx, stop := signal.NotifyContext(cmdCtx(cmd), os.Interrupt, syscall.SIGTERM)
	defer stop()
	results, runErr := workspace.CheckWatches(ctx, o.WS, args, r, time.Now())
	if runErr != nil && len(results) == 0 {
		return runErr // an unknown name or an immediate cancel: nothing ran
	}
	out := cmd.OutOrStdout()
	if asJSON {
		if results == nil {
			results = []workspace.WatchResult{}
		}
		if err := encodeJSON(out, results); err != nil {
			return err
		}
	} else {
		printWatchReport(out, results)
	}
	for _, res := range results {
		if res.HistoryError != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: the check of %s was not recorded: %s\n", esc(res.Watch), esc(res.HistoryError))
		}
	}
	if runErr != nil {
		return runErr
	}
	return watchExit(results)
}

func printWatchReport(out io.Writer, results []workspace.WatchResult) {
	if len(results) == 0 {
		fmt.Fprintln(out, "No watches. Add files under watches/ (see "+docRef("watches")+").")
		return
	}
	var ok, alert, errs int
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tWATCH\tREASON")
	for _, r := range results {
		switch r.Status {
		case watches.StatusOK:
			ok++
		case watches.StatusAlert:
			alert++
		default:
			errs++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Status, esc(r.Watch), esc(r.Reason))
	}
	_ = tw.Flush()
	fmt.Fprintf(out, "\n%d ok, %d alert%s, %d error%s\n", ok, alert, plural(alert), errs, plural(errs))
	for _, r := range results {
		printSample(out, r)
	}
}

// printSample lists the rows an alerting rows check read. They are shown here
// and nowhere else.
func printSample(out io.Writer, r workspace.WatchResult) {
	if r.Sample == nil || len(r.Sample.Rows) == 0 {
		return
	}
	cols := make([]string, len(r.Sample.Columns))
	for i, c := range r.Sample.Columns {
		cols[i] = esc(c)
	}
	fmt.Fprintf(out, "\n%s: first %d row%s (%s)\n", esc(r.Watch), len(r.Sample.Rows), plural(len(r.Sample.Rows)), strings.Join(cols, ", "))
	for _, row := range r.Sample.Rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = esc(c)
		}
		fmt.Fprintf(out, "  %s\n", strings.Join(cells, " | "))
	}
	if r.Sample.Capped {
		fmt.Fprintln(out, "  ... (more rows or columns than shown)")
	}
}

// watchExit is the exit status for a set of results: 1 when any alert fired,
// else 2 when any watch errored, else 0.
func watchExit(results []workspace.WatchResult) error {
	var alert, errs int
	for _, r := range results {
		switch r.Status {
		case watches.StatusAlert:
			alert++
		case watches.StatusError:
			errs++
		}
	}
	switch {
	case alert > 0:
		return &exitCodeError{code: checkExitFail, msg: fmt.Sprintf("%d of %d watches fired", alert, len(results))}
	case errs > 0:
		return &exitCodeError{code: checkExitError, msg: fmt.Sprintf("%d of %d watches could not be checked", errs, len(results))}
	}
	return nil
}

// --- history ------------------------------------------------------------------

func runWatchHistory(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 0 {
		return errors.New("--limit must be 0 (all) or more")
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	entries, err := ws.WatchHistory().Entries(cmdCtx(cmd), name)
	if err != nil {
		return err
	}
	slices.Reverse(entries) // the file is oldest first
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := cmd.OutOrStdout()
	if asJSON {
		return encodeJSON(out, entries)
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "No checks recorded. Run: slaw watch check")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tSTATUS\tWATCH\tREASON")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", whenText(&e.At, "-"), e.Status, esc(e.Watch), esc(e.Reason))
	}
	return tw.Flush()
}
