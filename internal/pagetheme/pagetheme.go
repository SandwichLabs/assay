// Package pagetheme builds the host stylesheet of an HTML page
// (_slaw/theme.css): the person's theme roles as --slaw-* custom properties,
// plus the mode. Studio's page window builds it from the roles of the window
// the person is looking at; `slaw artifact render` has no window, so it uses
// DefaultRoles, the built-in light theme. A leaf: standard library only.
package pagetheme

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Path is where the stylesheet is served, relative to the page.
const Path = "_slaw/theme.css"

// ThemeTypeRole is the pseudo-role that carries the mode ("light" or "dark").
const ThemeTypeRole = "--slaw-theme-type"

var (
	nameRe  = regexp.MustCompile(`^--slaw-[a-z0-9][a-z0-9-]{0,40}$`)
	valueRe = regexp.MustCompile(`^[A-Za-z0-9 #(),.%/'"_+-]{1,160}$`)
)

// Stylesheet builds _slaw/theme.css from the roles a theme set. Only a
// --slaw-* name with a plain value gets through; a value that could close a
// declaration, open a function that loads something or unbalance a quote is
// dropped, so this is not a way to write CSS.
func Stylesheet(roles map[string]string) string {
	names := make([]string, 0, len(roles))
	for n := range roles {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(":root{")
	scheme := ""
	for _, n := range names {
		v := strings.TrimSpace(roles[n])
		if n == ThemeTypeRole {
			if v == "light" || v == "dark" {
				scheme = v
			}
			continue
		}
		if !nameRe.MatchString(n) || !safeValue(v) {
			continue
		}
		fmt.Fprintf(&b, "%s:%s;", n, v)
	}
	if scheme != "" {
		// Also a property of its own: a page stylesheet that sets color-scheme
		// replaces the host's, so this is how a page still reads the mode.
		fmt.Fprintf(&b, "color-scheme:%s;%s:%s;", scheme, ThemeTypeRole, scheme)
	}
	b.WriteString("}\n")
	return b.String()
}

func safeValue(v string) bool {
	if !valueRe.MatchString(v) || strings.Count(v, `"`)%2 != 0 || strings.Count(v, "'")%2 != 0 {
		return false
	}
	l := strings.ToLower(v)
	return !strings.Contains(l, "url") && !strings.Contains(l, "expression") && !strings.Contains(l, "image") && !strings.Contains(l, "//") && !strings.Contains(l, "/*")
}

// LinkInto puts a link to the stylesheet at the start of the document (see
// insertAtStart), so the page's own styles come after it and win.
func LinkInto(doc []byte) []byte { return insertAtStart(doc, []byte(linkTag)) }

const linkTag = `<link rel="stylesheet" href="` + Path + `">`

// docStartRe is the leading comments and doctype of a document, which the host's
// tags must follow (anything before a doctype puts the page in quirks mode).
var docStartRe = regexp.MustCompile(`(?is)^(?:\s*<!--.*?-->)*\s*<!doctype[^>]*>`)

// insertAtStart puts tags right after a leading doctype, or at the very start
// of a document without one. It does not look for a <head>: such a search can
// be fooled by an attribute value or by content before the head, and the HTML
// parser puts a <meta> or <link> that comes first into the implied head anyway.
func insertAtStart(doc, tags []byte) []byte {
	at := 0
	if loc := docStartRe.FindIndex(doc); loc != nil {
		at = loc[1]
	}
	var b bytes.Buffer
	b.Grow(len(doc) + len(tags))
	b.Write(doc[:at])
	b.Write(tags)
	b.Write(doc[at:])
	return b.Bytes()
}

// ExportCSP is the Content-Security-Policy an exported static page carries as a
// <meta> tag: no script, no network, only its own files, no form posts. Studio's
// frame gets the same limits from response headers; a folder opened from disk
// has no such headers.
const ExportCSP = "default-src 'none'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; form-action 'none'; base-uri 'none'"

// ForExport adds to a finished static page's document the CSP <meta> and then
// the link to the stylesheet, at the very start of the document (after a
// doctype), so the CSP is in force before anything the page wrote and the page
// cannot move it out of effect: the parser puts them in the implied head.
func ForExport(doc []byte) []byte {
	meta := `<meta http-equiv="Content-Security-Policy" content="` + ExportCSP + `">`
	return insertAtStart(doc, []byte(meta+linkTag))
}

// DefaultRoles is the built-in light theme (SchoolHouse, the app's default) as
// the roles Stylesheet takes: every colour and font role, and the mode. The
// frontend's theme/builtin.ts is the source; a test keeps the two in step.
func DefaultRoles() map[string]string {
	out := make(map[string]string, len(defaultRoles)+1)
	for k, v := range defaultRoles {
		out[k] = v
	}
	out[ThemeTypeRole] = "light"
	return out
}

var defaultRoles = map[string]string{
	"--slaw-bg":                "#FAF7F0",
	"--slaw-bg-alt":            "#F2EDDF",
	"--slaw-surface":           "#FFFFFF",
	"--slaw-fg":                "#1F2330",
	"--slaw-fg-muted":          "#5C6275",
	"--slaw-fg-disabled":       "#A8A89C",
	"--slaw-border":            "#1F2330",
	"--slaw-border-subtle":     "#D6D3CA",
	"--slaw-shadow":            "#3F4350",
	"--slaw-focus":             "#1F7268",
	"--slaw-accent":            "#1F7268",
	"--slaw-accent-hover":      "#3FA39B",
	"--slaw-accent-active":     "#236962",
	"--slaw-accent-fg":         "#FAF7F0",
	"--slaw-highlight":         "#F2A93B",
	"--slaw-highlight-fg":      "#1F2330",
	"--slaw-attention":         "#E27262",
	"--slaw-splash":            "#C25A1F",
	"--slaw-success":           "#2F7D4A",
	"--slaw-warn":              "#B47800",
	"--slaw-error":             "#B23B2E",
	"--slaw-info":              "#2F6FA8",
	"--slaw-selection":         "#F2A93B",
	"--slaw-selection-fg":      "#1F2330",
	"--slaw-scrim":             "#1F23308C",
	"--slaw-chrome-bg":         "#1F2330",
	"--slaw-chrome-fg":         "#FAF7F0",
	"--slaw-chrome-border":     "#1F2330",
	"--slaw-sidebar-bg":        "#F2EDDF",
	"--slaw-sidebar-fg":        "#1F2330",
	"--slaw-sidebar-active-bg": "#FFFFFF",
	"--slaw-sidebar-active-fg": "#1F7268",
	"--slaw-code-bg":           "#1F2330",
	"--slaw-code-fg":           "#FAF7F0",
	"--slaw-editor-bg":         "#FAF7F0",
	"--slaw-editor-fg":         "#1F2330",
	"--slaw-editor-gutter-bg":  "#F2EDDF",
	"--slaw-editor-gutter-fg":  "#5C6275",
	"--slaw-editor-line":       "#1F72681A",
	"--slaw-editor-selection":  "#1F72682E",
	"--slaw-editor-cursor":     "#1F2330",
	"--slaw-syntax-keyword":    "#A84815",
	"--slaw-syntax-string":     "#2F7D4A",
	"--slaw-syntax-number":     "#2F6FA8",
	"--slaw-syntax-comment":    "#5C6275",
	"--slaw-syntax-link":       "#1F7268",
	"--slaw-syntax-heading":    "#1F2330",
	"--slaw-syntax-meta":       "#5C6275",
	"--slaw-chart-1":           "#1F7268",
	"--slaw-chart-2":           "#F2A93B",
	"--slaw-chart-3":           "#E27262",
	"--slaw-chart-4":           "#2F6FA8",
	"--slaw-chart-5":           "#7A4FA0",
	"--slaw-chart-6":           "#A65A2C",
	"--slaw-font-display":      "'Fraunces', Georgia, 'Times New Roman', serif",
	"--slaw-font-body":         "'Inter', system-ui, -apple-system, sans-serif",
	"--slaw-font-mono":         "'JetBrains Mono', 'SF Mono', Menlo, monospace",
}
