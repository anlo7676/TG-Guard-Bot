package store

import (
	"math"
	"testing"
)

func TestSQLJSONPropagatesEncodingFailure(t *testing.T) {
	if _, err := SQLJSON(map[string]any{"unsupported": math.NaN()}).Value(); err == nil {
		t.Fatal("encoding failure lost")
	}
}
