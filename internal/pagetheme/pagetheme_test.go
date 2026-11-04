package pagetheme

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStylesheet_OnlyPlainRolesGetThrough(t *testing.T) {
	css := Stylesheet(map[string]string{
		"--slaw-bg": "#fff", "--slaw-font-body": `"Fraunces", serif`, ThemeTypeRole: "dark",
		"--slaw-evil":  "url(http://x)",
		"--other":      "red",
		"--slaw-quote": `"open`,
	})
	assert.Contains(t, css, "--slaw-bg:#fff;")
	assert.Contains(t, css, `--slaw-font-body:"Fraunces", serif;`)
	assert.Contains(t, css, "color-scheme:dark;--slaw-theme-type:dark;")
	assert.NotContains(t, css, "evil")
	assert.NotContains(t, css, "--other")
	assert.NotContains(t, css, "quote")
	assert.NotContains(t, Stylesheet(map[string]string{ThemeTypeRole: "sepia"}), "theme-type")
}

func TestLinkInto(t *testing.T) {
	got := string(LinkInto([]byte(`<html><head><title>x</title></head></html>`)))
	assert.Equal(t, `<html><head><link rel="stylesheet" href="_slaw/theme.css"><title>x</title></head></html>`, got)
	assert.Equal(t, "<p>no head</p>", string(LinkInto([]byte("<p>no head</p>"))))
}

// DefaultRoles is the frontend's built-in light theme: this fails when
// theme/builtin.ts or the default fonts in theme/schema.ts change and the Go
// copy does not.
func TestDefaultRoles_MatchTheFrontendLightTheme(t *testing.T) {
	src, err := os.ReadFile("../../studio/frontend/src/theme/builtin.ts")
	require.NoError(t, err)
	text := string(src)
	start := strings.Index(text, "export const SCHOOLHOUSE_LIGHT")
	end := strings.Index(text, "export const SCHOOLHOUSE_DARK")
	require.True(t, start >= 0 && end > start)
	want := map[string]string{}
	block := text[start:end]
	matches := regexp.MustCompile(`'([a-z0-9.]+)':\s*'(#[0-9A-Fa-f]+)'`).FindAllStringSubmatch(block, -1)
	// Every "'role':" line of the light colours was read (none missed by the pattern).
	assert.Equal(t, len(regexp.MustCompile(`(?m)^\s*'[a-z0-9.]+':`).FindAllString(block, -1)), len(matches))
	for _, m := range matches {
		want["--slaw-"+strings.ReplaceAll(m[1], ".", "-")] = m[2]
	}
	schema, err := os.ReadFile("../../studio/frontend/src/theme/schema.ts")
	require.NoError(t, err)
	// The colour keys are exactly COLOR_ROLES, named as cssVarName does.
	rolesBlock := string(schema)[strings.Index(string(schema), "export const COLOR_ROLES = ["):]
	rolesBlock = rolesBlock[:strings.Index(rolesBlock, "] as const")]
	var roles, got []string
	for _, m := range regexp.MustCompile(`(?m)^\s*'([a-z0-9.]+)',`).FindAllStringSubmatch(rolesBlock, -1) {
		roles = append(roles, "--slaw-"+strings.ReplaceAll(m[1], ".", "-"))
	}
	require.NotEmpty(t, roles)
	for k := range want {
		got = append(got, k)
	}
	assert.ElementsMatch(t, roles, got, "the light theme sets exactly COLOR_ROLES")
	fonts := regexp.MustCompile("(?m)^  (display|body|mono): \"(.*)\",$").FindAllStringSubmatch(string(schema), -1)
	require.Len(t, fonts, 3)
	for _, m := range fonts {
		want["--slaw-font-"+m[1]] = m[2]
	}
	want[ThemeTypeRole] = "light"
	assert.Equal(t, want, DefaultRoles())
}

func TestDefaultRoles_AllSurviveTheFilter(t *testing.T) {
	roles := DefaultRoles()
	css := Stylesheet(roles)
	for name := range roles {
		if name == ThemeTypeRole {
			continue
		}
		assert.Contains(t, css, name+":", "%s was dropped by the filter", name)
	}
	assert.Contains(t, css, "color-scheme:light;--slaw-theme-type:light;")
}

func TestForExport_CSPFirstThenTheme(t *testing.T) {
	meta := `<meta http-equiv="Content-Security-Policy" content="` + ExportCSP + `">`
	link := `<link rel="stylesheet" href="_slaw/theme.css">`
	got := string(ForExport([]byte(`<!doctype html><html><head><title>x</title></head><body></body></html>`)))
	assert.Equal(t, `<!doctype html><html><head>`+meta+link+`<title>x</title></head><body></body></html>`, got)
}

func TestForExport_DocumentsWithoutAHead(t *testing.T) {
	for _, in := range []string{`<html><body>b</body></html>`, `<!doctype html><p>b</p>`, `<p>b</p>`} {
		got := string(ForExport([]byte(in)))
		assert.Contains(t, got, ExportCSP, in)
		assert.Contains(t, got, `href="_slaw/theme.css"`, in)
		assert.Less(t, strings.Index(got, "Content-Security-Policy"), strings.Index(got, "b</"), in)
	}
}
