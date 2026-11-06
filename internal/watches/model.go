// Package watches is the standing questions of a workspace as reviewed files:
// a question (a saved query or a metric) and a condition, kept as
// watches/<name>.yaml beside the queries and metrics they ask. Checking a
// watch runs the question once through the person's read-only path
// (workspace.CheckWatches) and records the outcome in a machine-local history
// that never leaves .slaw-local/.
//
// The package owns the model, the strict decoder and validator, the Store (a
// FileStore over the folder and an in-memory Mock) and the history store. It
// decodes with internal/strictyaml, so a file from a cloned repository or a
// bundle is held to the same rules as a semantics file: byte cap before parse,
// no anchors, aliases, tags or merge keys, unknown keys refused. It runs
// nothing: the policy, not the file, decides what a check may execute.
// Reference: docs/reference/watches.md.
package watches

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/slaw/internal/filestore"
)

// Folder layout.
const (
	// Dir is the workspace folder; it holds one yaml file per watch, named
	// <name>.yaml.
	Dir       = "watches"
	Extension = ".yaml"
)

// Caps. A watch is a few lines; these are generous for a person and small
// enough that a hostile folder costs neither memory nor a long message.
const (
	MaxYAMLBytes   = 16 << 10
	MaxYAMLNodes   = 500
	MaxFiles       = 500
	MaxDescription = 200
	MaxParams      = 20
	MaxParamValue  = 200
)

// The row conditions of a watch's when block.
const (
	RowsAny  = "any"
	RowsNone = "none"
)

// Errors a caller can test with errors.Is.
var (
	// ErrInvalid wraps every structural refusal: a document that must not be
	// stored or used, so a caller can tell it from I/O and conflicts.
	ErrInvalid = errors.New("invalid watch")
	// ErrConflict means the file changed on disk since it was loaded.
	ErrConflict = filestore.ErrConflict
	// ErrNotFound means there is no such watch.
	ErrNotFound = filestore.ErrNotFound
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// notFoundError is what Get and Delete return for an unknown name; it
// satisfies errors.Is(err, ErrNotFound).
type notFoundError struct{ name string }

func (e *notFoundError) Error() string        { return fmt.Sprintf("watch %q not found", e.name) }
func (e *notFoundError) Is(target error) bool { return target == ErrNotFound }

// Meta is what a store adds to a watch it loaded; it is never part of the file.
type Meta struct {
	// Rev is the content hash of the one file as loaded; hand it back on a
	// write and a stale write is ErrConflict.
	Rev string `yaml:"-" json:"rev,omitempty"`
	// Error is set for a file that could not be read (bad yaml, too large, a
	// name that is not valid): it still lists, under its file stem, so it can
	// be found and fixed, and it is never saved over without Overwrite.
	Error string `yaml:"-" json:"error,omitempty"`
}

// Version is the only document version there is. A file without a version
// field is version 1.
const Version = 1

// Watch is one standing question with a condition. Its name is the file name.
type Watch struct {
	// Version is optional: absent means 1; any other value is refused.
	Version int `yaml:"version,omitempty" json:"version,omitempty"`
	// Name is the file stem; it is not a field of the document.
	Name        string    `yaml:"-" json:"name"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	Question    Question  `yaml:"question" json:"question"`
	When        Condition `yaml:"when" json:"when"`

	Meta `yaml:"-"`
}

// Question is what a watch asks: exactly one of a saved query (by id, with
// parameter values bound by name) or a metric (run at its grain total, on the
// connection of its saved query).
type Question struct {
	// Query is a saved query id (queries/<id>.sql).
	Query string `yaml:"query,omitempty" json:"query,omitempty"`
	// Params are the values for the saved query's declared parameters, as the
	// text a person would give to `slaw query --saved -p name=value`. They
	// reach SQL only as bound parameters.
	Params map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
	// Metric is a metric name (semantics/metrics/<name>.yaml).
	Metric string `yaml:"metric,omitempty" json:"metric,omitempty"`
}

// Target is the saved query id or metric name the question asks.
func (q Question) Target() string {
	if q.Query != "" {
		return q.Query
	}
	return q.Metric
}

// Condition is when a watch fires: exactly one of rows, above, below or
// outside.
type Condition struct {
	// Rows is "any" (fires when the result has rows) or "none".
	Rows string `yaml:"rows,omitempty" json:"rows,omitempty"`
	// Above fires when the value is greater than the threshold.
	Above *Threshold `yaml:"above,omitempty" json:"above,omitempty"`
	// Below fires when the value is less than the threshold.
	Below *Threshold `yaml:"below,omitempty" json:"below,omitempty"`
	// Outside fires when the value is below the first or above the second.
	Outside Range `yaml:"outside,omitempty" json:"outside,omitempty"`
}

// IsThreshold reports whether the condition reads one numeric value.
func (c Condition) IsThreshold() bool { return c.Rows == "" }

// Describe is the condition as a short phrase: "above 1,000", "outside 10 to
// 90", "any rows".
func (c Condition) Describe() string {
	switch {
	case c.Rows == RowsAny:
		return "any rows"
	case c.Rows == RowsNone:
		return "no rows"
	case c.Above != nil:
		return "above " + c.Above.Display()
	case c.Below != nil:
		return "below " + c.Below.Display()
	case len(c.Outside) == 2:
		return "outside " + c.Outside[0].Display() + " to " + c.Outside[1].Display()
	}
	return ""
}

// Threshold is a number in a condition. It is read strictly: a finite decimal
// written plainly (-5, 0, 1000, 0.95). A quoted "100", 1e3, 0x10, +5, .inf and
// .nan are refused rather than coerced, so what the file says is what is
// compared.
type Threshold struct {
	text string
	val  float64
}

var thresholdRE = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

const thresholdHint = "a threshold must be a plain decimal number such as 0, -5 or 99.5, not quoted and without an exponent"

// MaxThresholdLen is the longest a threshold may be written. Integer and
// decimal values are compared with it exactly, so there is no digit limit
// beyond this sanity cap.
const MaxThresholdLen = 40

// ParseThreshold reads a threshold written as a plain decimal.
func ParseThreshold(text string) (Threshold, error) {
	if !thresholdRE.MatchString(text) {
		return Threshold{}, fmt.Errorf("%s (got %q)", thresholdHint, text)
	}
	if len(text) > MaxThresholdLen {
		return Threshold{}, fmt.Errorf("a threshold is at most %d characters (got %d)", MaxThresholdLen, len(text))
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return Threshold{}, fmt.Errorf("%q is not a finite number", text)
	}
	if f == 0 {
		text = strings.TrimPrefix(text, "-") // -0 is 0
	}
	return Threshold{text: text, val: f}, nil
}

// Rat is the threshold exactly, for comparing with an integer or decimal
// value too big for a float64.
func (t Threshold) Rat() *big.Rat {
	r, ok := new(big.Rat).SetString(t.text)
	if !ok {
		return new(big.Rat)
	}
	return r
}

// Float is the threshold as a float64.
func (t Threshold) Float() float64 { return t.val }

// String is the number as written in the file.
func (t Threshold) String() string { return t.text }

// Display is the number with thousands separators, as a reason shows it.
// It is the text as written, exact however many digits, with trailing zeros of
// the fraction dropped.
func (t Threshold) Display() string {
	s := t.text
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return groupDecimal(s)
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (t *Threshold) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || (n.Tag != "!!int" && n.Tag != "!!float") {
		return fmt.Errorf("line %d: %s (got %q)", n.Line, thresholdHint, n.Value)
	}
	v, err := ParseThreshold(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*t = v
	return nil
}

// MarshalYAML writes the number as it was read, never with an exponent.
func (t Threshold) MarshalYAML() (any, error) {
	// No tag: yaml resolves the digits itself (!!int, or !!float for an integer
	// beyond uint64), so the encoder never forces a tag the text does not have.
	return &yaml.Node{Kind: yaml.ScalarNode, Value: t.text}, nil
}

// MarshalJSON writes the number as a JSON number.
func (t Threshold) MarshalJSON() ([]byte, error) { return []byte(t.text), nil }

// Range is the two ends of an outside condition: [lo, hi].
type Range []Threshold

// MarshalYAML writes the pair in flow style: outside: [10, 90].
func (r Range) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, t := range r {
		v, err := t.MarshalYAML()
		if err != nil {
			return nil, err
		}
		child, _ := v.(*yaml.Node)
		n.Content = append(n.Content, child)
	}
	return n, nil
}

// FormatNumber writes f for a person: thousands separators, no exponent, at
// most six decimals with trailing zeros dropped. A non-zero number is never
// shown as 0: one too small for six decimals is written in full.
func FormatNumber(f float64) string {
	s := strconv.FormatFloat(f, 'f', 6, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if (s == "0" || s == "-0") && f != 0 {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return groupDecimal(s)
}

// FormatRat writes an exact integer in full with thousands separators and
// anything else as FormatNumber does.
func FormatRat(r *big.Rat) string {
	if r.IsInt() {
		return groupDecimal(r.Num().String())
	}
	f, _ := r.Float64()
	return FormatNumber(f)
}

// groupDecimal puts thousands separators in the integer part of a plain
// decimal ("-1234567.5" is "-1,234,567.5") and writes "-0" as "0".
func groupDecimal(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg && out != "0" {
		out = "-" + out
	}
	return out
}
