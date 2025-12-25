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

	t.Run("depth_limit", func(t *testing.T) {
		parser := NewParser(fields)
		deepQuery := strings.Repeat("(", 25) + "name:test" + strings.Repeat(")", 25)
		_, _, err := parser.ParseToSQL(deepQuery)
		if err == nil || !strings.Contains(err.Error(), "nesting too deep") {
			t.Errorf("Expected depth limit error, got: %v", err)
		}
	})

	t.Run("term_count_limit", func(t *testing.T) {
		parser := NewParser(fields)
		parser.MaxTerms = 5 // Lower limit for testing
		manyTerms := "name:a AND name:b AND name:c AND name:d AND name:e AND name:f"
		_, _, err := parser.ParseToSQL(manyTerms)
		if err == nil || !strings.Contains(err.Error(), "too many terms") {
			t.Errorf("Expected term count error, got: %v", err)
		}
	})

	t.Run("enhanced_parser_query_length", func(t *testing.T) {
		parser := NewParser(fields)
		longQuery := strings.Repeat("name:test && ", 1000) // Enhanced syntax
		_, _, err := parser.ParseToSQL(longQuery)
		if err == nil || !strings.Contains(err.Error(), "query too long") {
			t.Errorf("Expected query length error for enhanced parser, got: %v", err)
		}
	})

	t.Run("enhanced_parser_depth_limit", func(t *testing.T) {
		parser := NewParser(fields)
		deepQuery := strings.Repeat("(", 25) + "name:test" + strings.Repeat(")", 25)
		_, _, err := parser.ParseToSQL(deepQuery)
		if err == nil || !strings.Contains(err.Error(), "nesting too deep") {
			t.Errorf("Expected depth limit error for enhanced parser, got: %v", err)
		}
	})

	t.Run("enhanced_parser_term_count", func(t *testing.T) {
		parser := NewParser(fields)
		parser.MaxTerms = 2

		// Query with exactly 2 terms should succeed
		query := "name:john && email:test"
		_, _, err := parser.ParseToSQL(query)
		if err != nil {
			t.Errorf("Should allow 2 terms, got: %v", err)
		}

		// Query with 3 terms should fail
		query = "name:john && email:test && status:active"
		_, _, err = parser.ParseToSQL(query)
		if err == nil || !strings.Contains(err.Error(), "too many terms") {
			t.Errorf("Expected term count error for enhanced parser with 3 terms, got: %v", err)
		}
	})

	t.Run("configurable_limits", func(t *testing.T) {
		parser := NewParser(fields)
		// Test that limits are configurable
		parser.MaxQueryLength = 50
		parser.MaxDepth = 3
		parser.MaxTerms = 2

		// Should fail with short limit
		_, _, err := parser.ParseToSQL("name:test AND email:test AND status:active")
		if err == nil || !strings.Contains(err.Error(), "too many terms") {
			t.Errorf("Expected configurable term limit to work, got: %v", err)
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
}
