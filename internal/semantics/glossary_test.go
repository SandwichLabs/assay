package semantics

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetGlossaryTerm(t *testing.T) {
	out, replaced := SetGlossaryTerm("", "Churn", "Customers who left.")
	assert.False(t, replaced)
	assert.Equal(t, "## Churn\n\nCustomers who left.\n", out)

	out, replaced = SetGlossaryTerm(out, "ARR", "Annual recurring revenue.")
	assert.False(t, replaced)
	assert.Equal(t, "## Churn\n\nCustomers who left.\n\n## ARR\n\nAnnual recurring revenue.\n", out)

	// replace in place, case-insensitively, keeping the neighbours
	out, replaced = SetGlossaryTerm(out+"\n## Last\n\nz\n", "churn", "Left within 30 days.")
	assert.True(t, replaced)
	assert.Equal(t, "## churn\n\nLeft within 30 days.\n\n## ARR\n\nAnnual recurring revenue.\n\n## Last\n\nz\n", out)

	body, ok := GlossarySection(out, "ARR")
	assert.True(t, ok)
	assert.Equal(t, "Annual recurring revenue.", body)
	_, ok = GlossarySection(out, "nothing")
	assert.False(t, ok)

	// a preamble without a trailing newline still gets a blank line
	out, _ = SetGlossaryTerm("# Glossary", "A", "b")
	assert.Equal(t, "# Glossary\n\n## A\n\nb\n", out)
	g, err := DecodeGlossary([]byte(out))
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, g.Terms)
}

func TestSetGlossaryTerm_HeadingsInFencesAreNotTerms(t *testing.T) {
	text := "## One\n\n```\n## not a term\n```\n\n## Two\n\nb\n"
	out, replaced := SetGlossaryTerm(text, "One", "new")
	assert.True(t, replaced)
	assert.Equal(t, "## One\n\nnew\n\n## Two\n\nb\n", out)
}

func TestValidateGlossaryTerm(t *testing.T) {
	assert.NoError(t, ValidateGlossaryTerm("Net revenue", "Gross less refunds.\n\n- a list\n"))
	assert.NoError(t, ValidateGlossaryTerm("C#", "a language")) // a # inside is fine
	for name, c := range map[string][2]string{
		"empty term":    {"", "x"},
		"multi-line":    {"a\nb", "x"},
		"padded":        {" a", "x"},
		"trailing hash": {"a #", "x"},
		"hidden":        {"a‮b", "x"},
		"empty text":    {"a", "  \n"},
		"heading":       {"a", "x\n## b\ny"},
		"open fence":    {"a", "x\n```\ncode"},
		"control":       {"a", "x\u0007"},
		"long term":     {strings.Repeat("a", MaxTerm+1), "x"},
		"long text":     {"a", strings.Repeat("x", MaxTermText+1)},
	} {
		assert.ErrorIs(t, ValidateGlossaryTerm(c[0], c[1]), ErrInvalid, name)
	}
}

func TestSetGlossaryTerm_KeepsCRLF(t *testing.T) {
	out, replaced := SetGlossaryTerm("## A\r\n\r\nold\r\n", "B", "new")
	assert.False(t, replaced)
	assert.Equal(t, "## A\r\n\r\nold\r\n\r\n## B\r\n\r\nnew\r\n", out)
	out, replaced = SetGlossaryTerm(out, "a", "newer")
	assert.True(t, replaced)
	assert.Equal(t, "## a\r\n\r\nnewer\r\n\r\n## B\r\n\r\nnew\r\n", out)
	// mixed endings are left as LF additions, not rewritten
	out, _ = SetGlossaryTerm("## A\r\n\r\nold\n", "B", "n")
	assert.Equal(t, "## A\r\n\r\nold\n\n## B\n\nn\n", out)
}

func TestGlossaryOpenFence(t *testing.T) {
	assert.True(t, GlossaryOpenFence("## A\n\n```\ncode\n"))
	assert.False(t, GlossaryOpenFence("## A\n\n```\ncode\n```\n"))
}

func TestGlossaryTermRev(t *testing.T) {
	text := "# Terms\n\n## A\n\nalpha\n\n## B\n\nbeta\n"
	a, ok := GlossaryTermRev(text, "a")
	require.True(t, ok)
	assert.True(t, IsTermRev(a))
	_, ok = GlossaryTermRev(text, "nothing")
	assert.False(t, ok)

	// another term changing, a term appended, or CRLF endings do not move it
	for _, other := range []string{
		"# Terms\n\n## A\n\nalpha\n\n## B\n\nCHANGED\n",
		text + "\n## C\n\ngamma\n",
		strings.ReplaceAll(text, "\n", "\r\n"),
		text + "\n\n\n",
	} {
		got, ok := GlossaryTermRev(other, "A")
		require.True(t, ok)
		assert.Equal(t, a, got)
	}
	// its own text moves it
	got, _ := GlossaryTermRev("## A\n\nalpha2\n", "A")
	assert.NotEqual(t, a, got)
}

func TestGlossaryTermRev_LastSectionAndRawSection(t *testing.T) {
	text := "## A\n\nalpha\n\n## B\n\nbeta"
	b, ok := GlossaryTermRev(text, "B")
	require.True(t, ok)
	got, _ := GlossaryTermRev(text+"\n", "B")
	assert.Equal(t, b, got, "a final newline does not move the last term's rev")
	got, _ = GlossaryTermRev("## A\n\nalpha\n\n## B\n\nbeta\n\n\n", "b")
	assert.Equal(t, b, got)
	raw, ok := GlossaryRawSection("## A\n\nalpha\n\n## Bee\n\nbeta\n", "bee")
	assert.True(t, ok)
	assert.Equal(t, "## Bee\n\nbeta\n", raw)
}
