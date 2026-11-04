package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnAdd_PresetFillsEndpointOptions(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runSlaw("conn", "add", "fl", "--type", "s3", "--prefix", "s3://lake/", "--preset", "floci", "--scope", "shared", "--workspace", root)
	require.NoError(t, err, stderr)
	data := readSharedConfig(t, root)
	assert.Contains(t, data, "localhost:4566")
	assert.Contains(t, data, "url_style: path")
}

func TestConnAdd_PresetExplicitOptionWinsAndRegionFillsB2(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runSlaw("conn", "add", "fl", "--type", "s3", "--prefix", "s3://lake/", "--preset", "floci", "--option", "endpoint=localhost:4567", "--scope", "shared", "--workspace", root)
	require.NoError(t, err, stderr)
	data := readSharedConfig(t, root)
	assert.Contains(t, data, "localhost:4567")
	assert.NotContains(t, data, "localhost:4566")

	_, stderr, err = runSlaw("conn", "add", "bb", "--type", "s3", "--prefix", "s3://b2bucket/", "--preset", "b2", "--region", "us-west-004", "--scope", "shared", "--workspace", root)
	require.NoError(t, err, stderr)
	data = readSharedConfig(t, root)
	assert.Contains(t, data, "s3.us-west-004.backblazeb2.com")
	assert.Contains(t, data, "url_style: vhost")
}

func TestConnAdd_PresetErrors(t *testing.T) {
	root := newWS(t)
	_, _, err := runSlaw("conn", "add", "x", "--type", "s3", "--prefix", "s3://b/", "--preset", "nope", "--scope", "shared", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aws, minio, b2, floci, other")

	_, _, err = runSlaw("conn", "add", "x", "--type", "s3", "--prefix", "s3://b/", "--preset", "b2", "--scope", "shared", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a region")

	_, _, err = runSlaw("conn", "add", "x", "--type", "s3", "--prefix", "s3://b/", "--preset", "b2", "--region", "a/b", "--scope", "shared", "--workspace", root)
	require.Error(t, err)

	_, _, err = runSlaw("conn", "add", "x", "--type", "gcs", "--prefix", "gcs://b/", "--preset", "minio", "--scope", "shared", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--preset is for s3")
}

func readSharedConfig(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "slaw.yaml"))
	require.NoError(t, err)
	return string(b)
}

func TestConnAdd_ReadOnlyFlagIsWrittenOnlyWhenGiven(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runSlaw("conn", "add", "explicit", "--type", "duckdb", "--path", "data/a.duckdb", "--read-only", "--scope", "shared", "--workspace", root)
	require.NoError(t, err, stderr)
	_, stderr, err = runSlaw("conn", "add", "implicit", "--type", "duckdb", "--path", "data/b.duckdb", "--scope", "shared", "--workspace", root)
	require.NoError(t, err, stderr)
	data := readSharedConfig(t, root)
	assert.Equal(t, 1, strings.Count(data, "read_only: true"), data)
	assert.Contains(t, data, "name: explicit")
}
