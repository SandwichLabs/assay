package watches

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/slaw/internal/connname"
	"github.com/SandwichLabs/slaw/internal/filestore"
	"github.com/SandwichLabs/slaw/internal/strictyaml"
)

// paramNameRE is the shape of a saved query's parameter name.
var paramNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

// Label is how a watch file is named in messages.
func Label(stem string) string { return Dir + "/" + stem + Extension }

// Decode parses and validates a watch file. stem is the file name without its
// extension and becomes the watch's name; it must be a valid name.
func Decode(stem string, data []byte) (*Watch, error) {
	label := Label(stem)
	if err := ValidateName(stem); err != nil {
		return nil, relabel(label, err)
	}
	var w Watch
	if err := strictyaml.Decode(data, MaxYAMLBytes, MaxYAMLNodes, label, &w); err != nil {
		return nil, invalid("%s", err.Error())
	}
	w.Name = stem
	if err := Validate(&w); err != nil {
		return nil, relabel(label, err)
	}
	return &w, nil
}

// relabel puts the file in front of a validator's message, keeping ErrInvalid
// as the one prefix.
func relabel(label string, err error) error {
	return invalid("%s: %s", label, errorText(err))
}

// errorText is an error's message without the "invalid watch:" prefix, which
// every listed Error would otherwise repeat.
func errorText(err error) string {
	return strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")
}

// Encode validates and renders a watch as file bytes.
func Encode(w *Watch) ([]byte, error) {
	if err := ValidateName(w.Name); err != nil {
		return nil, err
	}
	if err := Validate(w); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(w); err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", Label(w.Name), err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", Label(w.Name), err)
	}
	if buf.Len() > MaxYAMLBytes {
		return nil, invalid("%s would be %d bytes, over the %d byte limit", Label(w.Name), buf.Len(), MaxYAMLBytes)
	}
	return buf.Bytes(), nil
}

// ValidateName checks a watch name: the connection-name rule (lower-case
// letters, digits and _, at most 63 characters), and not a file name some
// systems reserve.
func ValidateName(name string) error {
	if !connname.Valid(name) {
		return invalid("watch name %q must start with a lower-case letter or underscore and use only lower-case letters, digits and underscores (at most 63 characters)", name)
	}
	if !strings.HasPrefix(name, "_") && !filestore.ValidID(name) {
		return invalid("watch name %q is a reserved file name; pick another", name)
	}
	return nil
}

// Validate checks everything the file format promises, with a message that
// says what to change. It does not read the workspace: whether the saved query
// or the metric exists is found when the watch is checked.
func Validate(w *Watch) error {
	if err := checkLine("description", w.Description, MaxDescription); err != nil {
		return err
	}
	if err := validateQuestion(&w.Question); err != nil {
		return err
	}
	return validateCondition(&w.When)
}

// checkLine refuses text that is too long, spans lines or holds a control or
// bidi character (a YAML escape only becomes one once decoded).
func checkLine(field, v string, max int) error {
	if n := utf8.RuneCountInString(v); n > max {
		return invalid("%s is %d characters, over the %d character limit", field, n, max)
	}
	if strings.ContainsAny(v, "\r\n") {
		return invalid("%s must be one line", field)
	}
	if r, bad := strictyaml.FirstHiddenRune(v, false); bad {
		return invalid("%s contains a control or invisible character (U+%04X)", field, r)
	}
	return nil
}

func validateQuestion(q *Question) error {
	switch {
	case q.Query == "" && q.Metric == "":
		return invalid("question needs exactly one of query (a saved query id) or metric (a metric name)")
	case q.Query != "" && q.Metric != "":
		return invalid("question has both query and metric; a watch asks exactly one question")
	}
	if q.Query != "" {
		return validateQueryQuestion(q)
	}
	if !connname.Valid(q.Metric) {
		return invalid("question.metric %q is not a valid metric name (lower-case letters, digits and _)", q.Metric)
	}
	if len(q.Params) > 0 {
		return invalid("question.params applies to a saved query; a metric takes none")
	}
	if q.Connection != "" && !connname.Valid(q.Connection) {
		return invalid("question.connection %q is not a valid connection name", q.Connection)
	}
	return nil
}

func validateQueryQuestion(q *Question) error {
	if !filestore.ValidID(q.Query) {
		return invalid("question.query %q is not a valid saved query id", q.Query)
	}
	if q.Connection != "" {
		return invalid("question.connection applies to a metric; a saved query runs on its own connection")
	}
	if len(q.Params) > MaxParams {
		return invalid("question.params has %d entries, over the limit of %d", len(q.Params), MaxParams)
	}
	names := make([]string, 0, len(q.Params))
	for n := range q.Params {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !paramNameRE.MatchString(n) {
			return invalid("question.params name %q is not a valid parameter name", n)
		}
		if err := checkLine(fmt.Sprintf("question.params.%s", n), q.Params[n], MaxParamValue); err != nil {
			return err
		}
	}
	return nil
}

func validateCondition(c *Condition) error {
	set := 0
	for _, on := range []bool{c.Rows != "", c.Above != nil, c.Below != nil, len(c.Outside) > 0} {
		if on {
			set++
		}
	}
	switch {
	case set == 0:
		return invalid("when needs exactly one condition: rows (any or none), above, below or outside")
	case set > 1:
		return invalid("when has more than one condition; a watch has exactly one of rows, above, below or outside")
	}
	if c.Rows != "" && c.Rows != RowsAny && c.Rows != RowsNone {
		return invalid("when.rows is %q; use any or none", c.Rows)
	}
	if len(c.Outside) > 0 {
		if len(c.Outside) != 2 {
			return invalid("when.outside needs exactly two numbers, [low, high] (got %d)", len(c.Outside))
		}
		if lo, hi := c.Outside[0].Float(), c.Outside[1].Float(); lo >= hi {
			return invalid("when.outside needs low < high (got [%s, %s])", c.Outside[0], c.Outside[1])
		}
	}
	return nil
}

// ReadableError is a store error as one sentence: the store wraps a decoder's
// refusal as `failed to get watch "x": invalid watch: <file>: <why>`, and the
// first two layers say nothing the caller does not already know.
func ReadableError(err error) string {
	s := err.Error()
	if strings.HasPrefix(s, "failed to get ") {
		if i := strings.Index(s, `": `); i >= 0 {
			s = s[i+3:]
		}
	}
	return strings.TrimPrefix(s, ErrInvalid.Error()+": ")
}
