package agents

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testdata/long_shape_views.json is read by this test and by
// studio/frontend/src/lib/notebookViews.spec.ts: the Go port and the TypeScript
// view picker must pick the same view for a (label, series, value) result.
type longShapeCase struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Want    string   `json:"want"`
}

func TestBestViewFor_LongShapeParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/long_shape_views.json")
	require.NoError(t, err)
	var cases []longShapeCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			assert.Equal(t, c.Want, bestViewFor(describeViewShape(c.Columns, c.Rows)))
		})
	}
}

// A long result is still a result the bar and line views fit, and the grouped
// bar and multi-line charts are reached by pinning the view, whatever it picked.
func TestCheckViewFit_LongShape(t *testing.T) {
	shape := describeViewShape([]string{"month", "rep", "revenue"}, [][]any{{"Jan", "Ana", 1}, {"Jan", "Ben", 2}, {"Feb", "Ana", 3}})
	require.NotNil(t, shape.long)
	assert.Equal(t, 2, shape.long.labels)
	assert.Equal(t, 2, shape.long.series)
	assert.True(t, shape.long.unique)
	for _, v := range []string{"bar", "line", "table"} {
		assert.True(t, checkViewFit(v, shape).OK, v)
	}
	assert.Nil(t, describeViewShape([]string{"a", "b"}, [][]any{{"x", 1}}).long)
}
