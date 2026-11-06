package docsref

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/watches"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// TestWatchesDocNamesTheWholeSurface keeps docs/reference/watches.md honest:
// every field of the watch file, the row conditions, the outcomes, the caps, the
// four commands and the exit statuses appear in it, and the doc is listed in
// every index.
func TestWatchesDocNamesTheWholeSurface(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "reference", "watches.md"))
	require.NoError(t, err)
	doc := string(b)

	for _, typ := range []reflect.Type{reflect.TypeOf(watches.Watch{}), reflect.TypeOf(watches.Question{}), reflect.TypeOf(watches.Condition{})} {
		for _, key := range yamlKeys(typ) {
			assert.True(t, strings.Contains(doc, "`"+key+"`") || strings.Contains(doc, "`question."+key+"`") || strings.Contains(doc, "`when."+key+"`"),
				"watches.md does not document the %s field %q", typ.Name(), key)
		}
	}
	for _, v := range []string{watches.RowsAny, watches.RowsNone, string(watches.StatusOK), string(watches.StatusAlert), string(watches.StatusError), watches.Dir, watches.HistoryPath, "history.lock"} {
		assert.True(t, strings.Contains(doc, v), "watches.md does not name %q", v)
	}
	for _, cmd := range []string{"slaw watch list", "slaw watch show", "slaw watch check", "slaw watch history", "--json", "--watches"} {
		assert.True(t, strings.Contains(doc, cmd), "watches.md does not document %q", cmd)
	}
	// The caps the doc states are the ones in the code.
	for what, want := range map[string]string{
		"file size":            strconv.Itoa(watches.MaxYAMLBytes>>10) + " KiB",
		"node cap":             strconv.Itoa(watches.MaxYAMLNodes),
		"files":                strconv.Itoa(watches.MaxFiles),
		"description":          strconv.Itoa(watches.MaxDescription),
		"params":               strconv.Itoa(watches.MaxParams),
		"param value":          strconv.Itoa(watches.MaxParamValue),
		"entries per watch":    strconv.Itoa(watches.DefaultPerWatch),
		"history size":         strconv.Itoa(watches.DefaultMaxBytes>>20) + " MB",
		"reason":               strconv.Itoa(watches.MaxReasonRunes),
		"rows read":            "1,001",
		"rows shown":           "1,000+",
		"threshold rows read":  "**5** rows",
		"statement timeout":    "2 minute",
		"sample rows":          "20 rows",
		"sample cell":          "80 characters",
		"exit status alert":    "`1` at least one alert fired",
		"exit status error":    "`2` none fired but some watch could not be checked",
		"exit status all ok":   "`0` every watch is `ok`",
		"the sample of a rows": "never written to the history",
	} {
		assert.True(t, strings.Contains(doc, want), "watches.md does not state the %s (%q)", what, want)
	}
	assert.Equal(t, 2*60, int(workspace.CheckStatementTimeout.Seconds()), "update watches.md when the statement timeout changes")

	for _, index := range []string{
		filepath.Join("docs", "reference", "README.md"),
		filepath.Join("docs", "README.md"),
	} {
		b, err := os.ReadFile(filepath.Join(root, index))
		require.NoError(t, err)
		assert.True(t, strings.Contains(string(b), "watches.md"), "%s does not list watches.md", index)
	}
	ws, err := os.ReadFile(filepath.Join(root, "docs", "reference", "workspaces.md"))
	require.NoError(t, err)
	assert.Contains(t, string(ws), "watches/<name>.yaml", "workspaces.md does not list the watches bundle entry")
	assert.Contains(t, string(ws), "slaw watch", "workspaces.md does not list the command")
	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Contains(t, string(claude), "`watch` (`list`, `show`, `check`, `history`", "CLAUDE.md does not list the CLI commands")
}
