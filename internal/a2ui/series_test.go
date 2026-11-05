package a2ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_MultiSeriesLineAndBar(t *testing.T) {
	ok := func(c string) {
		t.Helper()
		_, err := Validate(`{"title":"x","components":[` + c + `]}`)
		require.NoError(t, err, c)
	}
	bad := func(c, want string) {
		t.Helper()
		_, err := Validate(`{"title":"x","components":[` + c + `]}`)
		require.Error(t, err, c)
		assert.Contains(t, err.Error(), want, c)
	}
	// A long or wide result, inline or bound, on both charts.
	for _, typ := range []string{"lineChart", "barChart"} {
		ok(`{"id":"c","type":"` + typ + `","columns":["m","rep","revenue"],"rows":[["Jan","Ana",1],["Jan","Ben",2]]}`)
		ok(`{"id":"c","type":"` + typ + `","sql":"SELECT m, rep, sum(x) FROM t GROUP BY 1, 2"}`)
		bad(`{"id":"c","type":"`+typ+`","rows":[["Jan",{"a":1}]]}`, "components[0].rows[0][1]")
	}
	// Inline series on lineChart.
	ok(`{"id":"c","type":"lineChart","series":[{"name":"Ana","points":[{"label":"Jan","value":1}]},{"name":"Ben","points":[]}]}`)
	// ...which barChart does not take.
	bad(`{"id":"c","type":"barChart","series":[]}`, "components[0].series")
	bad(`{"id":"c","type":"lineChart","series":"x"}`, "components[0].series: expected array")
	bad(`{"id":"c","type":"lineChart","series":["x"]}`, "components[0].series[0]: expected an object")
	bad(`{"id":"c","type":"lineChart","series":[{"name":"A","points":[],"color":"yellow"}]}`, "components[0].series[0].color: a series has no such field")
	bad(`{"id":"c","type":"lineChart","series":[{"points":[]}]}`, "components[0].series[0].name")
	bad(`{"id":"c","type":"lineChart","series":[{"name":" ","points":[]}]}`, "components[0].series[0].name")
	bad(`{"id":"c","type":"lineChart","series":[{"name":"A","points":[]},{"name":"A","points":[]}]}`, "used by an earlier series")
	bad(`{"id":"c","type":"lineChart","series":[{"name":"A"}]}`, "components[0].series[0].points: expected array")
	bad(`{"id":"c","type":"lineChart","series":[{"name":"A","points":[{"label":"a","value":"x"}]}]}`, "components[0].series[0].points[0].value")
	bad(`{"id":"c","type":"lineChart","series":[{"name":"`+strings.Repeat("n", 81)+`","points":[]}]}`, "longer than 80")
}

func TestValidate_OneDataSource(t *testing.T) {
	check := func(c string) error {
		_, err := Validate(`{"title":"x","components":[` + c + `]}`)
		return err
	}
	err := check(`{"id":"c","type":"lineChart","points":[{"label":"a","value":1}],"series":[]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use one data source, got points and series")
	err = check(`{"id":"c","type":"barChart","columns":["a","b"],"rows":[["x",1]],"sql":"SELECT 1"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rows and sql")
	// a bound chart that also carries old inline points still passes
	require.NoError(t, check(`{"id":"c","type":"lineChart","points":[],"sql":"SELECT 1"}`))
	err = check(`{"id":"c","type":"lineChart","series":[{"name":"A","points":[]},{"name":"A ","points":[]}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "used by an earlier series")
}

func TestValidate_InlineSeriesCaps(t *testing.T) {
	series := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"name":"s` + strconv.Itoa(i) + `","points":[]}`
		}
		return `{"title":"x","components":[{"id":"c","type":"lineChart","series":[` + strings.Join(parts, ",") + `]}]}`
	}
	_, err := Validate(series(12))
	require.NoError(t, err)
	_, err = Validate(series(13))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "13 series is over the limit of 12")

	pts := make([]string, 501)
	for i := range pts {
		pts[i] = `{"label":"a","value":1}`
	}
	_, err = Validate(`{"title":"x","components":[{"id":"c","type":"lineChart","series":[{"name":"A","points":[` + strings.Join(pts, ",") + `]}]}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "501 points is over the limit of 500")
}

func TestPromptBlock_DocumentsSeries(t *testing.T) {
	p := PromptBlock()
	assert.Contains(t, p, "series([{name, points}])")
	assert.Contains(t, p, "(label, series, value)")
	assert.Contains(t, p, "bars sum the rest into Other")
}
