package pagetheme

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
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

func TestLinkInto_GoesFirstAfterTheDoctype(t *testing.T) {
	link := `<link rel="stylesheet" href="_slaw/theme.css">`
	assert.Equal(t, `<!doctype html>`+link+`<html><head></head></html>`, string(LinkInto([]byte(`<!doctype html><html><head></head></html>`))))
	assert.Equal(t, link+"<p>no doctype</p>", string(LinkInto([]byte("<p>no doctype</p>"))))
	assert.Equal(t, "<!-- c -->\n<!DOCTYPE html>"+link+"<p>x</p>", string(LinkInto([]byte("<!-- c -->\n<!DOCTYPE html><p>x</p>"))))
}

// The CSP meta must end up in the document's head and before everything the
// page wrote, whatever the page's markup does to look like it has a head.
func TestForExport_TheCSPMetaIsFirstInTheHead(t *testing.T) {
	for name, in := range map[string]string{
		"plain":               `<!doctype html><html><head><title>x</title></head><body><p id="p">b</p></body></html>`,
		"no head":             `<!doctype html><html><body><p id="p">b</p></body></html>`,
		"no html, no doctype": `<p id="p">b</p>`,
		"head in attribute":   `<!doctype html><html data-x="<head>"><head><title>x</title></head><body><p id="p">b</p></body></html>`,
		"head after body":     `<!doctype html><p id="p">early</p><head><meta http-equiv="x"></head>`,
		"leading comment":     "<!-- c --><!doctype html><html><head></head><body><p id=\"p\">b</p></body></html>",
	} {
		out := ForExport([]byte(in))
		doc, err := html.Parse(bytes.NewReader(out))
		require.NoError(t, err, name)

		var metaNode *html.Node
		var order []*html.Node // elements in document order
		var walk func(n *html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				order = append(order, n)
				if n.Data == "meta" && attrOf(n, "http-equiv") == "Content-Security-Policy" && metaNode == nil {
					metaNode = n
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
		require.NotNil(t, metaNode, name)
		assert.Equal(t, ExportCSP, attrOf(metaNode, "content"), name)
		require.NotNil(t, metaNode.Parent, name)
		assert.Equal(t, "head", metaNode.Parent.Data, name)
		// Only html and head may come before it: nothing the page wrote.
		for _, n := range order {
			if n == metaNode {
				break
			}
			assert.Contains(t, []string{"html", "head"}, n.Data, "%s: <%s> comes before the CSP meta", name, n.Data)
		}
		assert.Contains(t, string(out), `href="_slaw/theme.css"`, name)
	}
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
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
