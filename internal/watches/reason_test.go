package watches

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanDBError(t *testing.T) {
	msg := "query execution failed: failed to execute query on duckdb: Conversion Error: Could not convert string 'x' to INT32\n\nLINE 1: SELECT CAST(a AS INT)\n               ^"
	assert.Equal(t, "Conversion Error: Could not convert string 'x' to INT32", CleanDBError(msg))
	assert.Equal(t, "Binder Error: no column", CleanDBError("query execution failed: could not validate query: Binder Error: no column"))
	assert.Equal(t, "plain", CleanDBError("  plain  "))
	assert.Equal(t, "Conversion Error: Could not convert string 'acme' to INT32",
		CleanDBError("failed to execute duckdb query: failed to run query: Conversion Error: Could not convert string 'acme' to INT32"))
	assert.Equal(t, `failed to connect "x": no`, CleanDBError(`failed to connect "x": no`), "a layer with a name in it is not a wrapper")
}

func TestErrorClass_IsOnlyEverAClassName(t *testing.T) {
	for msg, want := range map[string]string{
		"query execution failed: failed to execute query on duckdb: Conversion Error: Could not convert string 'alice@example.com' to INT32": "Conversion Error",
		"failed to execute duckdb query: failed to run query: Conversion Error: Could not convert string 'acme' to INT32":                    "Conversion Error",
		"Out of Memory Error: failed to allocate":             "Out of Memory Error",
		"Binder Error: Referenced column \"email\" not found": "Binder Error",
		"alice@example.com exploded":                          "database error",
		"'alice' Error: x":                                    "database error",
		"":                                                    "database error",
		"conversion error: lower case is not a class":         "database error",
		"Invalid Input Error: value 'secret' is not a number": "Invalid Input Error",
	} {
		got := ErrorClass(msg)
		assert.Equal(t, want, got, msg)
		assert.NotContains(t, got, "'")
		assert.NotContains(t, got, "@")
	}
}
