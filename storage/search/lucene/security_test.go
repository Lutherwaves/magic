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

	t.Run("nesting_depth_limit", func(t *testing.T) {
		parser := NewParser(fields)
		parser.MaxDepth = 3 // Set low limit for testing

		// Create a deeply nested query: (((name:test)))
		deepQuery := strings.Repeat("(", 5) + "name:test" + strings.Repeat(")", 5)
		_, _, err := parser.ParseToSQL(deepQuery)
		if err == nil || !strings.Contains(err.Error(), "nesting depth") {
			t.Errorf("Expected nesting depth error, got: %v", err)
		}
	})

	t.Run("term_count_limit", func(t *testing.T) {
		parser := NewParser(fields)
		parser.MaxTerms = 5 // Set low limit for testing

		// Create a query with many terms
		terms := []string{"name:test1", "name:test2", "name:test3", "name:test4", "name:test5", "name:test6"}
		manyTermsQuery := strings.Join(terms, " AND ")
		_, _, err := parser.ParseToSQL(manyTermsQuery)
		if err == nil || !strings.Contains(err.Error(), "terms exceeds maximum") {
			t.Errorf("Expected term count error, got: %v", err)
		}
	})

	t.Run("quoted_phrases_count_as_single_term", func(t *testing.T) {
		parser := NewParser(fields)
		parser.MaxTerms = 2

		// "test value" should count as 1 term, not multiple
		_, _, err := parser.ParseToSQL(`name:"test value"`)
		if err != nil {
			t.Errorf("Quoted phrase should count as single term, got error: %v", err)
		}
	})

	t.Run("range_queries_count_as_single_term", func(t *testing.T) {
		parser := NewParser(fields)
		parser.MaxTerms = 2

		// [min TO max] should count as 1 term
		_, _, err := parser.ParseToSQL("name:[a TO z]")
		if err != nil {
			t.Errorf("Range query should count as single term, got error: %v", err)
		}
	})
}
