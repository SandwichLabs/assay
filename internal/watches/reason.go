package watches

import (
	"regexp"
	"strings"
)

// A database error can quote a value of the row it choked on ("Could not
// convert string 'alice@example.com' to INT32"). The report that ran the check
// may show it; the history must not keep it. CleanDBError is the message for
// the report; ErrorClass is all the history keeps of it.

// dbWrapperRE matches one layer the engine and the policy put around a
// database's own message: "query execution failed: ", "failed to execute
// duckdb query: ", "failed to run query: ", "could not validate query: ".
var dbWrapperRE = regexp.MustCompile(`^(?:failed to [a-z][a-z ]*|query execution failed|could not validate query): `)

// CleanDBError is a database error as one line without the wrapper chain and
// without the "LINE n:" excerpt of the statement.
func CleanDBError(msg string) string {
	msg = strings.TrimSpace(msg)
	for {
		loc := dbWrapperRE.FindStringIndex(msg)
		if loc == nil {
			break
		}
		msg = strings.TrimSpace(msg[loc[1]:])
	}
	if i := strings.Index(msg, "\n"); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	return msg
}

// errorClassRE is DuckDB's stable error-class name: letters and single spaces
// only ("Conversion Error", "Out of Memory Error"), so it can hold no value.
var errorClassRE = regexp.MustCompile(`^([A-Z][A-Za-z]+(?: [A-Za-z]+){0,3} Error)\b`)

// ErrorClass is the error class at the start of a database error, or
// "database error". It is fixed wording from a closed shape, never a value.
func ErrorClass(msg string) string {
	if m := errorClassRE.FindStringSubmatch(CleanDBError(msg)); m != nil {
		return m[1]
	}
	return "database error"
}
