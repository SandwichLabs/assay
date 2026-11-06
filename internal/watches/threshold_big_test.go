package watches

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An integer beyond int64 and uint64 is written without a forced !!int tag and
// read back as written (yaml resolves those digits as !!float).
func TestThreshold_HugeIntegersRoundTrip(t *testing.T) {
	for _, text := range []string{"123456789012345678901", "18446744073709551616", "-9223372036854775809", "9223372036854775808", "0.5", "-12"} {
		th, err := ParseThreshold(text)
		require.NoError(t, err, text)
		w := &Watch{Name: "big", Question: Question{Metric: "m"}, When: Condition{Above: &th}}
		data, err := Encode(w)
		require.NoError(t, err, text)
		assert.NotContains(t, string(data), "!!", text)
		got, err := Decode("big", data)
		require.NoError(t, err, "%s:\n%s", text, data)
		assert.Equal(t, text, got.When.Above.String())

		w = &Watch{Name: "band", Question: Question{Metric: "m"}, When: Condition{Outside: Range{{text: "-99999999999999999999999", val: -1e23}, th}}}
		data, err = Encode(w)
		require.NoError(t, err, text)
		got, err = Decode("band", data)
		require.NoError(t, err, "%s:\n%s", text, data)
		assert.Equal(t, text, got.When.Outside[1].String())
	}
}

func TestThreshold_DisplayIsExact(t *testing.T) {
	th, err := ParseThreshold("1.00000000000000000000001")
	require.NoError(t, err)
	assert.Equal(t, "1.00000000000000000000001", th.Display())
	th, _ = ParseThreshold("1234567.50")
	assert.Equal(t, "1,234,567.5", th.Display())
	th, _ = ParseThreshold("-0.0000001")
	assert.Equal(t, "-0.0000001", th.Display())
	th, _ = ParseThreshold("1000")
	assert.Equal(t, "1,000", th.Display())
}

func TestValidate_OutsideOrderIsExact(t *testing.T) {
	lo, _ := ParseThreshold("100000000000000000000")
	hi, _ := ParseThreshold("100000000000000000001")
	assert.Equal(t, lo.Float(), hi.Float(), "the doubles collide")
	w := &Watch{Name: "w", Question: Question{Metric: "m"}, When: Condition{Outside: Range{lo, hi}}}
	assert.NoError(t, Validate(w), "low < high exactly")
	w.When.Outside = Range{hi, lo}
	assert.Error(t, Validate(w))
	w.When.Outside = Range{lo, lo}
	assert.Error(t, Validate(w))
}
