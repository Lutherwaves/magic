package lucene

import (
	"strings"
	"testing"
)

func TestSecurityLimits(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "email", IsJSONB: false},
	}

	t.Run("query_length_limit", func(t *testing.T) {
		parser := NewParser(fields)
		longQuery := strings.Repeat("name:test AND ", 1000) // ~14KB query
		_, _, err := parser.ParseToSQL(longQuery)
		if err == nil || !strings.Contains(err.Error(), "query too long") {
			t.Errorf("Expected query length error, got: %v", err)
		}
	})

	t.Run("configurable_query_length", func(t *testing.T) {
		parser := NewParser(fields)
		// Test that query length limit is configurable
		parser.MaxQueryLength = 20 // Very short limit

		// Should fail with short limit (this query is > 20 chars)
		_, _, err := parser.ParseToSQL("name:test AND email:testing")
		if err == nil || !strings.Contains(err.Error(), "query too long") {
			t.Errorf("Expected configurable query length limit to work, got: %v", err)
		}
	})

	t.Run("defaults_are_safe", func(t *testing.T) {
		parser := NewParser(fields)
		// Verify defaults are set
		if parser.MaxQueryLength != DefaultMaxQueryLength {
			t.Errorf("MaxQueryLength should default to %d, got %d", DefaultMaxQueryLength, parser.MaxQueryLength)
		}
		if parser.MaxDepth != DefaultMaxDepth {
			t.Errorf("MaxDepth should default to %d, got %d", DefaultMaxDepth, parser.MaxDepth)
		}
		if parser.MaxTerms != DefaultMaxTerms {
			t.Errorf("MaxTerms should default to %d, got %d", DefaultMaxTerms, parser.MaxTerms)
		}
	})

	t.Run("empty_query", func(t *testing.T) {
		parser := NewParser(fields)
		sql, params, err := parser.ParseToSQL("")
		if err != nil {
			t.Errorf("Empty query should not error, got: %v", err)
		}
		if sql != "" || len(params) != 0 {
			t.Errorf("Empty query should produce empty SQL, got sql=%v params=%v", sql, params)
		}
	})
}
