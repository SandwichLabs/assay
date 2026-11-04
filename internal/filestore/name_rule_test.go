package filestore

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

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
