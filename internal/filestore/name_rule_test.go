package filestore

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Studio frontend checks names before it calls a binding; its pattern must
// stay equal to idRE or a name Go accepts is refused in the UI (#322).
func TestFrontendNameRuleMatchesIDRE(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "studio", "frontend", "src", "lib", "newItem.ts"))
	require.NoError(t, err)
	m := regexp.MustCompile("const NAME_RE = /(.+)/\n").FindSubmatch(src)
	require.NotNil(t, m, "NAME_RE not found in newItem.ts")
	require.Equal(t, idRE.String(), string(m[1]))
}

// The reserved-name rule in newItem.ts must agree with ValidID on the Windows
// device names and the names next to them.
func TestFrontendReservedRuleMatchesValidID(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "studio", "frontend", "src", "lib", "newItem.ts"))
	require.NoError(t, err)
	m := regexp.MustCompile("const RESERVED_RE = /(.+)/\n").FindSubmatch(src)
	require.NotNil(t, m, "RESERVED_RE not found in newItem.ts")
	reserved := regexp.MustCompile(string(m[1]))
	for _, id := range []string{"con", "prn", "aux", "nul", "com1", "com9", "com0", "lpt1", "lpt9", "conx", "com10", "sales", "my_com1"} {
		assert.Equal(t, !ValidID(id), reserved.MatchString(id) && idRE.MatchString(id), id)
	}
}
