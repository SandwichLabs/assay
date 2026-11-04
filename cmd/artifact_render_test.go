package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

const renderManifest = `version: 1
title: Regions
view: html
parameters:
  - name: min
    type: integer
    min: 0
    max: 100
    default: "0"
queries:
  - name: rows
    sql: SELECT * FROM (VALUES ('east', 10), ('west', 3), ('north', 7)) AS t(region, n) WHERE n >= $min ORDER BY region
`

const renderIndex = `<!doctype html>
<html><head><meta charset="utf-8"><title>Regions</title>
<link rel="stylesheet" href="assets/style.css"></head>
<body><h1>At least {{ param "min" }}</h1>
<ul>{{ range (query "rows").Rows }}<li>{{ index . 0 }}={{ index . 1 }}</li>{{ end }}</ul>
<img src="assets/logo.svg"></body></html>`

const renderLogo = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`

// seedRenderPage writes a static page called "pg" and returns its folder.
func seedRenderPage(t *testing.T, ws string) string {
	t.Helper()
	dir := filepath.Join(ws, "artifacts", "pg")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "html", "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(renderManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte(renderIndex), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "assets", "style.css"), []byte("body { color: var(--slaw-fg, CanvasText); }"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "assets", "logo.svg"), []byte(renderLogo), 0o644))
	return dir
}

func allowPage(t *testing.T, ws, name string) {
	t.Helper()
	out, stderr, err := runSlaw("artifact", "view", "status", name, "--json", "--workspace", ws)
	require.NoError(t, err, stderr)
	hash := hashFromStatusJSON(t, out)
	_, stderr, err = runSlaw("artifact", "view", "allow", name, "--hash", hash, "--yes", "--workspace", ws)
	require.NoError(t, err, stderr)
}

func hashFromStatusJSON(t *testing.T, out string) string {
	t.Helper()
	var st struct {
		Hash string `json:"hash"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &st))
	require.NotEmpty(t, st.Hash)
	return st.Hash
}

func TestArtifactRender_AStaticPageIsDrawnWithItsQueryResults(t *testing.T) {
	ws := newWS(t)
	seedRenderPage(t, ws)
	allowPage(t, ws, "pg")

	out := filepath.Join(t.TempDir(), "site")
	stdout, stderr, err := runSlaw("artifact", "render", "pg", "--out", out, "-p", "min=5", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Rendered pg to ")
	assert.Contains(t, stdout, "index.html")

	doc, err := os.ReadFile(filepath.Join(out, "index.html"))
	require.NoError(t, err)
	html := string(doc)
	assert.Contains(t, html, "At least 5")
	assert.Contains(t, html, "<li>east=10</li>")
	assert.Contains(t, html, "<li>north=7</li>")
	assert.NotContains(t, html, "west", "the parameter filtered the rows in SQL")
	assert.Contains(t, html, `<link rel="stylesheet" href="_slaw/theme.css">`)
	meta := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; form-action 'none'; base-uri 'none'">`
	assert.Contains(t, html, meta)
	assert.Less(t, strings.Index(html, meta), strings.Index(html, `href="assets/style.css"`), "the CSP comes before the page's own head")

	theme, err := os.ReadFile(filepath.Join(out, "_slaw", "theme.css"))
	require.NoError(t, err)
	assert.Contains(t, string(theme), "--slaw-bg:#FAF7F0;")
	assert.Contains(t, string(theme), "color-scheme:light;--slaw-theme-type:light;")

	css, err := os.ReadFile(filepath.Join(out, "assets", "style.css"))
	require.NoError(t, err)
	assert.Contains(t, string(css), "CanvasText")
	svg, err := os.ReadFile(filepath.Join(out, "assets", "logo.svg"))
	require.NoError(t, err)
	assert.Equal(t, renderLogo, string(svg))
	assert.NoFileExists(t, filepath.Join(out, "index.html.tmpl"), "templates are not shipped")
}

func TestArtifactRender_DefaultFolderAndDefaultParameters(t *testing.T) {
	ws := newWS(t)
	seedRenderPage(t, ws)
	allowPage(t, ws, "pg")
	cwd := t.TempDir()
	t.Chdir(cwd)

	_, stderr, err := runSlaw("artifact", "render", "pg", "--workspace", ws)
	require.NoError(t, err, stderr)
	doc, err := os.ReadFile(filepath.Join(cwd, "pg-render", "index.html"))
	require.NoError(t, err)
	assert.Contains(t, string(doc), "<li>west=3</li>")
}

func TestArtifactRender_RefusesAScriptedPage(t *testing.T) {
	ws := newWS(t)
	dir := seedRenderPage(t, ws)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "app.js"), []byte("1"), 0o644))
	allowPage(t, ws, "pg")

	out := filepath.Join(t.TempDir(), "site")
	_, _, err := runSlaw("artifact", "render", "pg", "--out", out, "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has script")
	assert.NoDirExists(t, out)
}

func TestArtifactRender_RefusesAnA2UIViewAndAScriptView(t *testing.T) {
	ws := newWS(t)
	seedArtifacts(t, ws)
	seedScriptArtifact(t, ws)
	for name, want := range map[string]string{"sales": "A2UI view", "chart": "script view"} {
		out := filepath.Join(t.TempDir(), name)
		_, _, err := runSlaw("artifact", "render", name, "--out", out, "--workspace", ws)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), want, name)
		assert.Contains(t, err.Error(), "static view: html pages", name)
		assert.NoDirExists(t, out)
	}
}

func TestArtifactRender_RefusesAPageThatIsNotAllowed(t *testing.T) {
	ws := newWS(t)
	seedRenderPage(t, ws)

	out := filepath.Join(t.TempDir(), "site")
	_, _, err := runSlaw("artifact", "render", "pg", "--out", out, "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not been allowed")
	assert.Contains(t, err.Error(), "slaw artifact view allow pg")
	assert.NoDirExists(t, out)

	_, _, err = runSlaw("artifact", "view", "deny", "pg", "--workspace", ws)
	require.NoError(t, err)
	_, _, err = runSlaw("artifact", "render", "pg", "--out", out, "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "you denied it")
	assert.NoDirExists(t, out)
}

func TestArtifactRender_RefusesAPageThatChangedAfterItWasAllowed(t *testing.T) {
	ws := newWS(t)
	dir := seedRenderPage(t, ws)
	allowPage(t, ws, "pg")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "assets", "style.css"), []byte("body { color: red; }"), 0o644))

	out := filepath.Join(t.TempDir(), "site")
	_, _, err := runSlaw("artifact", "render", "pg", "--out", out, "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since you allowed it")
	assert.Contains(t, err.Error(), "slaw artifact view allow pg")
	assert.NoDirExists(t, out)

	// A changed query ends the allow too.
	allowPage(t, ws, "pg")
	manifest := filepath.Join(dir, "artifact.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte(renderManifest+"  - name: more\n    sql: SELECT 1\n"), 0o644))
	_, _, err = runSlaw("artifact", "render", "pg", "--out", out, "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since you allowed it")
}

func TestArtifactRender_RefusesABadParameter(t *testing.T) {
	ws := newWS(t)
	seedRenderPage(t, ws)
	allowPage(t, ws, "pg")

	want := map[string]string{
		"not a number": "must be a whole number", "out of range": "above its maximum", "unknown name": "no parameter named",
		"given twice": "more than once", "missing equals": "want name=value", "empty parameter": "want name=value",
	}
	for name, args := range map[string][]string{
		"not a number":    {"-p", "min=lots"},
		"out of range":    {"-p", "min=1000"},
		"unknown name":    {"-p", "nope=1"},
		"given twice":     {"-p", "min=1", "-p", "min=2"},
		"missing equals":  {"-p", "min"},
		"empty parameter": {"-p", "=3"},
	} {
		out := filepath.Join(t.TempDir(), "site")
		_, _, err := runSlaw(append([]string{"artifact", "render", "pg", "--out", out, "--workspace", ws}, args...)...)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), want[name], name)
		assert.NoDirExists(t, out, name)
	}
}

func TestArtifactRender_ASQLValueIsBoundNotSpliced(t *testing.T) {
	ws := newWS(t)
	dir := seedRenderPage(t, ws)
	m := "version: 1\ntitle: T\nview: html\nparameters:\n  - name: who\n    type: text\n    max_length: 40\n    default: \"\"\nqueries:\n  - name: q\n    sql: SELECT $who AS who\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(m), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte(`<p>{{ index (index (query "q").Rows 0) 0 }}</p>`), 0o644))
	require.NoError(t, os.Remove(filepath.Join(dir, "html", "assets", "style.css")))
	require.NoError(t, os.Remove(filepath.Join(dir, "html", "assets", "logo.svg")))
	allowPage(t, ws, "pg")

	out := filepath.Join(t.TempDir(), "site")
	_, stderr, err := runSlaw("artifact", "render", "pg", "--out", out, "-p", "who=x'; DROP TABLE t; --", "--workspace", ws)
	require.NoError(t, err, stderr)
	doc, err := os.ReadFile(filepath.Join(out, "index.html"))
	require.NoError(t, err)
	assert.Contains(t, string(doc), "DROP TABLE t")
}

func TestArtifactRender_OutputFolderRules(t *testing.T) {
	ws := newWS(t)
	seedRenderPage(t, ws)
	allowPage(t, ws, "pg")

	// A folder with files in it is refused, and left alone.
	busy := t.TempDir()
	keep := filepath.Join(busy, "keep.txt")
	require.NoError(t, os.WriteFile(keep, []byte("mine"), 0o644))
	_, _, err := runSlaw("artifact", "render", "pg", "--out", busy, "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
	assert.NoFileExists(t, filepath.Join(busy, "index.html"))

	// An empty one is fine; --force writes into a busy one and touches only its own files.
	_, stderr, err := runSlaw("artifact", "render", "pg", "--out", t.TempDir(), "--workspace", ws)
	require.NoError(t, err, stderr)
	_, stderr, err = runSlaw("artifact", "render", "pg", "--out", busy, "--force", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.FileExists(t, filepath.Join(busy, "index.html"))
	b, _ := os.ReadFile(keep)
	assert.Equal(t, "mine", string(b))

	// A file in the way, a link as the folder: refused.
	file := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, _, err = runSlaw("artifact", "render", "pg", "--out", file, "--force", "--workspace", ws)
	require.Error(t, err)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(t.TempDir(), link))
	_, _, err = runSlaw("artifact", "render", "pg", "--out", link, "--force", "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link")
}

func TestArtifactRender_NameCannotLeaveTheWorkspace(t *testing.T) {
	ws := newWS(t)
	for _, name := range []string{"../x", "a/b", ".."} {
		_, _, err := runSlaw("artifact", "render", name, "--workspace", ws)
		require.Error(t, err, name)
	}
}

func TestWriteRenderFiles_StaysInsideTheFolder(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	outside := t.TempDir()

	for _, rel := range []string{"../escape.txt", "/abs.txt", "a/../../escape.txt"} {
		err := writeRenderFiles(dest, []renderFile{{rel: "ok.txt", data: []byte("1")}, {rel: rel, data: []byte("x")}})
		require.Error(t, err, rel)
		assert.NoFileExists(t, filepath.Join(dest, "ok.txt"), "%s: paths are checked before the first write", rel)
	}

	// A link inside the folder is not followed, for a folder or for a file.
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(dest, "sub")))
	require.Error(t, writeRenderFiles(dest, []renderFile{{rel: "sub/f.txt", data: []byte("x")}}))
	assert.NoFileExists(t, filepath.Join(outside, "f.txt"))

	victim := filepath.Join(outside, "victim.txt")
	require.NoError(t, os.WriteFile(victim, []byte("safe"), 0o644))
	require.NoError(t, os.Symlink(victim, filepath.Join(dest, "index.html")))
	require.NoError(t, writeRenderFiles(dest, []renderFile{{rel: "index.html", data: []byte("new")}}))
	b, _ := os.ReadFile(victim)
	assert.Equal(t, "safe", string(b), "the link was replaced, not written through")
	b, _ = os.ReadFile(filepath.Join(dest, "index.html"))
	assert.Equal(t, "new", string(b))
}

// A hand-edited or pulled page whose query writes is held to the SQL policy at
// render time, as in Studio: nothing is drawn, written or created.
func TestArtifactRender_AQueryThatWritesIsRefusedAtRender(t *testing.T) {
	for name, sql := range map[string]string{
		"copy":   "COPY (SELECT 42) TO '%s/x.csv'",
		"insert": "INSERT INTO sales VALUES ('x', 1)",
	} {
		ws := newWS(t)
		_, stderr, err := runSlaw("query", "--workspace", ws, "CREATE TABLE sales AS SELECT 'east' AS region, 1 AS amount")
		require.NoError(t, err, stderr)
		dir := seedRenderPage(t, ws)
		victim := t.TempDir()
		stmt := sql
		if strings.Contains(sql, "%s") {
			stmt = strings.Replace(sql, "%s", victim, 1)
		}
		m := "version: 1\ntitle: T\nview: html\nqueries:\n  - name: q\n    sql: " + stmt + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(m), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte(`<p>{{ len (query "q").Rows }}</p>`), 0o644))
		require.NoError(t, os.Remove(filepath.Join(dir, "html", "assets", "style.css")))
		require.NoError(t, os.Remove(filepath.Join(dir, "html", "assets", "logo.svg")))
		allowPage(t, ws, "pg") // the person allowed what they saw; the policy still applies

		out := filepath.Join(t.TempDir(), "site")
		_, _, err = runSlaw("artifact", "render", "pg", "--out", out, "--workspace", ws)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "could not run", name)
		assert.NoDirExists(t, out, name)
		assert.NoFileExists(t, filepath.Join(victim, "x.csv"), name)
		rows, stderr, err := runSlaw("query", "--workspace", ws, "SELECT count(*) AS n FROM sales")
		require.NoError(t, err, stderr)
		assert.Contains(t, rows, "1", name)
		assert.NotContains(t, rows, "2", name)
	}
}

func TestArtifactRender_AMetricPageIsRefusedOnceTheMetricChanged(t *testing.T) {
	ws := newWS(t)
	_, stderr, err := runSlaw("query", "--workspace", ws, "CREATE TABLE sales AS SELECT * FROM (VALUES ('east', 10), ('west', 5)) t(region, amount)")
	require.NoError(t, err, stderr)
	w, err := workspace.Open(ws, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, w.Queries().Create(context.Background(), &queries.Query{ID: "base", Title: "base", SQL: "SELECT region, amount FROM sales"}))
	require.NoError(t, w.Close())
	metric := filepath.Join(ws, "semantics", "metrics", "revenue.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(metric), 0o755))
	require.NoError(t, os.WriteFile(metric, []byte("version: 1\nname: revenue\nquery: base\nmeasure: sum(amount)\ndimensions: [region]\n"), 0o644))

	dir := seedRenderPage(t, ws)
	m := "version: 1\ntitle: T\nview: html\nqueries:\n  - name: q\n    sql: SELECT * FROM metric('revenue')\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(m), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte(`<p>total={{ index (index (query "q").Rows 0) 0 }}</p>`), 0o644))
	require.NoError(t, os.Remove(filepath.Join(dir, "html", "assets", "style.css")))
	require.NoError(t, os.Remove(filepath.Join(dir, "html", "assets", "logo.svg")))
	allowPage(t, ws, "pg")

	out := filepath.Join(t.TempDir(), "site")
	_, stderr, err = runSlaw("artifact", "render", "pg", "--out", out, "--workspace", ws)
	require.NoError(t, err, stderr)
	doc, err := os.ReadFile(filepath.Join(out, "index.html"))
	require.NoError(t, err)
	assert.Contains(t, string(doc), "total=15")

	require.NoError(t, os.WriteFile(metric, []byte("version: 1\nname: revenue\nquery: base\nmeasure: sum(amount) * 2\ndimensions: [region]\n"), 0o644))
	out2 := filepath.Join(t.TempDir(), "site2")
	_, _, err = runSlaw("artifact", "render", "pg", "--out", out2, "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since you allowed it")
	assert.NoDirExists(t, out2)
}

func TestRenderedFiles_RefusesNamesThatDifferOnlyInCase(t *testing.T) {
	page := pageWithFiles(t, "assets/Logo.svg", "assets/logo.svg")
	_, err := renderedFiles(page, []byte("<p>x</p>"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without case")

	page = pageWithFiles(t, "_slaw/Theme.css")
	_, err = renderedFiles(page, []byte("<p>x</p>"))
	require.Error(t, err)
}

func TestWriteRenderFiles_ChecksEveryTargetBeforeTheFirstWrite(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(dest, "assets")))
	err := writeRenderFiles(dest, []renderFile{{rel: "index.html", data: []byte("x")}, {rel: "assets/a.css", data: []byte("y")}})
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(dest, "index.html"), "a later target's link stops the render before anything is written")

	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(t.TempDir(), link))
	require.Error(t, writeRenderFiles(link, []renderFile{{rel: "index.html", data: []byte("x")}}), "a folder that is a link is refused")
}

func pageWithFiles(t *testing.T, paths ...string) *artifacts.Artifact {
	t.Helper()
	a := &artifacts.Artifact{}
	for _, p := range paths {
		a.Files = append(a.Files, artifacts.PageFile{Path: p, Data: []byte("x")})
	}
	return a
}

func TestArtifactRender_AMissingArtifactReadsNaturally(t *testing.T) {
	ws := newWS(t)
	_, _, err := runSlaw("artifact", "render", "nope", "--out", filepath.Join(t.TempDir(), "o"), "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `artifact "nope"`)
	assert.NotContains(t, err.Error(), "static page")
}
