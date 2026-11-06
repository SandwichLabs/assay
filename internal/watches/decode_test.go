package watches

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode_AcceptedShapes(t *testing.T) {
	cases := map[string]struct {
		doc  string
		want func(t *testing.T, w *Watch)
	}{
		"saved query with params and rows any": {
			doc: "description: Orders stuck in pending for more than a day\nquestion:\n  query: stuck_orders\n  params: {days: 1, region: east}\nwhen:\n  rows: any\n",
			want: func(t *testing.T, w *Watch) {
				assert.Equal(t, "Orders stuck in pending for more than a day", w.Description)
				assert.Equal(t, "stuck_orders", w.Question.Query)
				assert.Equal(t, map[string]string{"days": "1", "region": "east"}, w.Question.Params, "a number is the text a -p would carry")
				assert.Equal(t, RowsAny, w.When.Rows)
				assert.False(t, w.When.IsThreshold())
				assert.Equal(t, "any rows", w.When.Describe())
			},
		},
		"metric above": {
			doc: "question:\n  metric: revenue\n  connection: warehouse\nwhen:\n  above: 1000\n",
			want: func(t *testing.T, w *Watch) {
				assert.Equal(t, "revenue", w.Question.Metric)
				assert.Equal(t, "warehouse", w.Question.Connection)
				require.NotNil(t, w.When.Above)
				assert.InDelta(t, 1000, w.When.Above.Float(), 0)
				assert.True(t, w.When.IsThreshold())
				assert.Equal(t, "above 1,000", w.When.Describe())
			},
		},
		"below with a fraction": {
			doc: "question: {query: rate}\nwhen: {below: 0.95}\n",
			want: func(t *testing.T, w *Watch) {
				assert.InDelta(t, 0.95, w.When.Below.Float(), 0)
				assert.Equal(t, "below 0.95", w.When.Describe())
			},
		},
		"outside": {
			doc: "question: {metric: m}\nwhen:\n  outside: [10, 90]\n",
			want: func(t *testing.T, w *Watch) {
				require.Len(t, w.When.Outside, 2)
				assert.Equal(t, "outside 10 to 90", w.When.Describe())
			},
		},
		"negative bounds": {
			doc: "question: {metric: m}\nwhen: {outside: [-5, -0.5]}\n",
			want: func(t *testing.T, w *Watch) {
				assert.Equal(t, "outside -5 to -0.5", w.When.Describe())
			},
		},
		"rows none and zero": {
			doc: "question: {query: heartbeat}\nwhen: {rows: none}\n",
			want: func(t *testing.T, w *Watch) {
				assert.Equal(t, "no rows", w.When.Describe())
			},
		},
		"zero threshold": {
			doc: "question: {query: q}\nwhen: {above: -0}\n",
			want: func(t *testing.T, w *Watch) {
				assert.Equal(t, "above 0", w.When.Describe())
			},
		},
		"comments are fine": {
			doc: "# why\nquestion:\n  query: q # the query\nwhen:\n  rows: any\n",
			want: func(t *testing.T, w *Watch) {
				assert.Equal(t, "q", w.Question.Query)
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w, err := Decode("w", []byte(c.doc))
			require.NoError(t, err)
			assert.Equal(t, "w", w.Name)
			c.want(t, w)
		})
	}
}

func TestDecode_Refusals(t *testing.T) {
	long := strings.Repeat("x", MaxDescription+1)
	manyParams := "question:\n  query: q\n  params:\n"
	for i := 0; i <= MaxParams; i++ {
		manyParams += "    p" + strings.Repeat("a", i) + ": 1\n"
	}
	cases := map[string]struct{ doc, want string }{
		"no question":              {"when: {rows: any}\n", "question needs exactly one of query"},
		"empty question":           {"question: {}\nwhen: {rows: any}\n", "question needs exactly one of query"},
		"query and metric":         {"question: {query: q, metric: m}\nwhen: {rows: any}\n", "has both query and metric"},
		"no condition":             {"question: {query: q}\n", "when needs exactly one condition"},
		"empty condition":          {"question: {query: q}\nwhen: {}\n", "when needs exactly one condition"},
		"two conditions":           {"question: {query: q}\nwhen: {rows: any, above: 1}\n", "more than one condition"},
		"above and below":          {"question: {query: q}\nwhen: {above: 1, below: 0}\n", "more than one condition"},
		"unknown rows":             {"question: {query: q}\nwhen: {rows: some}\n", `when.rows is "some"`},
		"quoted threshold":         {"question: {query: q}\nwhen: {above: \"100\"}\n", "plain decimal"},
		"exponent":                 {"question: {query: q}\nwhen: {above: 1e3}\n", "plain decimal"},
		"hex":                      {"question: {query: q}\nwhen: {above: 0x10}\n", "plain decimal"},
		"inf":                      {"question: {query: q}\nwhen: {above: .inf}\n", "plain decimal"},
		"nan":                      {"question: {query: q}\nwhen: {above: .nan}\n", "plain decimal"},
		"plus sign":                {"question: {query: q}\nwhen: {above: +5}\n", "plain decimal"},
		"underscore":               {"question: {query: q}\nwhen: {above: 1_000}\n", "plain decimal"},
		"leading dot":              {"question: {query: q}\nwhen: {above: .5}\n", "plain decimal"},
		"word":                     {"question: {query: q}\nwhen: {above: lots}\n", "plain decimal"},
		"outside reversed":         {"question: {query: q}\nwhen: {outside: [90, 10]}\n", "low < high"},
		"outside equal":            {"question: {query: q}\nwhen: {outside: [5, 5]}\n", "low < high"},
		"outside one":              {"question: {query: q}\nwhen: {outside: [5]}\n", "exactly two numbers"},
		"outside three":            {"question: {query: q}\nwhen: {outside: [1, 2, 3]}\n", "exactly two numbers"},
		"outside scalar":           {"question: {query: q}\nwhen: {outside: 5}\n", "cannot unmarshal"},
		"unknown field":            {"question: {query: q}\nwhen: {rows: any}\nschedule: hourly\n", "field schedule not found"},
		"unknown nested field":     {"question: {query: q, limit: 5}\nwhen: {rows: any}\n", "field limit not found"},
		"unknown condition":        {"question: {query: q}\nwhen: {equals: 5}\n", "field equals not found"},
		"params on a metric":       {"question: {metric: m, params: {a: 1}}\nwhen: {rows: any}\n", "a metric takes none"},
		"connection on a query":    {"question: {query: q, connection: c}\nwhen: {rows: any}\n", "a saved query runs on its own connection"},
		"bad query id":             {"question: {query: ../etc}\nwhen: {rows: any}\n", "not a valid saved query id"},
		"bad metric name":          {"question: {metric: Revenue}\nwhen: {rows: any}\n", "not a valid metric name"},
		"bad connection":           {"question: {metric: m, connection: \"a b\"}\nwhen: {rows: any}\n", "not a valid connection name"},
		"bad param name":           {"question: {query: q, params: {\"1x\": 1}}\nwhen: {rows: any}\n", "not a valid parameter name"},
		"param with newline":       {"question: {query: q, params: {a: \"x\\ny\"}}\nwhen: {rows: any}\n", "must be one line"},
		"param with control char":  {"question: {query: q, params: {a: \"x\\u0007\"}}\nwhen: {rows: any}\n", "control or invisible"},
		"long description":         {"description: " + long + "\nquestion: {query: q}\nwhen: {rows: any}\n", "over the 200 character limit"},
		"multi-line description":   {"description: \"a\\nb\"\nquestion: {query: q}\nwhen: {rows: any}\n", "must be one line"},
		"too many params":          {manyParams + "when: {rows: any}\n", "over the limit"},
		"anchor":                   {"question: &q {query: q}\nwhen: {rows: any}\n", "anchors"},
		"alias":                    {"a: &x {rows: any}\nquestion: {query: q}\nwhen: *x\n", "anchors"},
		"tag":                      {"question: {query: !!str q}\nwhen: {rows: any}\n", "tag"},
		"merge key":                {"question: {query: q}\nwhen:\n  <<: {rows: any}\n", "merge"},
		"two documents":            {"question: {query: q}\nwhen: {rows: any}\n---\nquestion: {query: q}\n", "single YAML document"},
		"duplicate key":            {"question: {query: q}\nquestion: {query: r}\nwhen: {rows: any}\n", "already defined"},
		"empty":                    {"", "empty"},
		"control character":        {"question: {query: q}\nwhen: {rows: any}\n# \x1b[31m\n", "control character"},
		"not a map":                {"- a\n- b\n", "cannot unmarshal"},
		"over the byte cap":        {"description: x\n" + strings.Repeat("# pad\n", MaxYAMLBytes/6+10), "byte limit"},
		"threshold as a sequence":  {"question: {query: q}\nwhen: {above: [1]}\n", "plain decimal"},
		"threshold with units":     {"question: {query: q}\nwhen: {above: 5%}\n", "plain decimal"},
		"threshold in a long form": {"question: {query: q}\nwhen: {above: 1234567890123456789}\n", "plain decimal"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode("w", []byte(c.doc))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalid), "every refusal is ErrInvalid: %v", err)
			assert.Contains(t, err.Error(), c.want)
			assert.Contains(t, err.Error(), "watches/w.yaml", "the message names the file")
		})
	}
}

func TestDecode_RefusesABadName(t *testing.T) {
	doc := []byte("question: {query: q}\nwhen: {rows: any}\n")
	for _, name := range []string{"Upper", "has space", "../x", "", "1abc", strings.Repeat("a", 64), "con", "nul"} {
		_, err := Decode(name, doc)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
	for _, name := range []string{"a", "_hidden", "stuck_orders", "w2", strings.Repeat("a", 63)} {
		_, err := Decode(name, doc)
		assert.NoError(t, err, name)
	}
}

func TestEncode_RoundTripsAndKeepsThresholdsPlain(t *testing.T) {
	src := "description: d\nquestion:\n  query: q\n  params:\n    days: \"1\"\nwhen:\n  outside: [0.25, 1000]\n"
	w, err := Decode("w", []byte(src))
	require.NoError(t, err)
	data, err := Encode(w)
	require.NoError(t, err)
	assert.Contains(t, string(data), "outside: [0.25, 1000]")
	assert.NotContains(t, string(data), "e+")
	back, err := Decode("w", data)
	require.NoError(t, err)
	assert.Equal(t, w.Question, back.Question)
	assert.Equal(t, w.When.Describe(), back.When.Describe())

	_, err = Encode(&Watch{Name: "w"})
	require.Error(t, err, "an invalid watch is not encoded")
	_, err = Encode(&Watch{Name: "BAD", Question: Question{Query: "q"}, When: Condition{Rows: RowsAny}})
	require.Error(t, err, "nor under an invalid name")
}

func TestFormatNumber(t *testing.T) {
	for in, want := range map[float64]string{
		0: "0", 1240: "1,240", 1000: "1,000", 999: "999", 1234567.5: "1,234,567.5", -1500: "-1,500",
		0.95: "0.95", 0.1 + 0.2: "0.3", 100: "100", -0.0000001: "0",
	} {
		assert.Equal(t, want, FormatNumber(in), "%v", in)
	}
}

func TestReadableError(t *testing.T) {
	err := errors.New(`failed to get watch "w": invalid watch: watches/w.yaml: when needs exactly one condition`)
	assert.Equal(t, "watches/w.yaml: when needs exactly one condition", ReadableError(err))
}
