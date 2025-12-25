package lucene

import (
	"strings"
	"testing"
)

// TestBasicFieldSearch tests basic field:value queries
func TestBasicFieldSearch(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "email", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name     string
		query    string
		wantSQL  string
		wantVals int
	}{
		{
			name:     "simple field query",
			query:    "name:john",
			wantSQL:  "name = ?",
			wantVals: 1,
		},
		{
			name:     "wildcard prefix",
			query:    "name:john*",
			wantSQL:  "name::text ILIKE ?",
			wantVals: 1,
		},
		{
			name:     "wildcard suffix",
			query:    "name:*john",
			wantSQL:  "name::text ILIKE ?",
			wantVals: 1,
		},
		{
			name:     "wildcard contains",
			query:    "name:*john*",
			wantSQL:  "name::text ILIKE ?",
			wantVals: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			if !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, tt.wantSQL)
			}
			if len(vals) != tt.wantVals {
				t.Errorf("ParseToSQL() vals count = %v, want %v", len(vals), tt.wantVals)
			}
		})
	}
}

// TestBooleanOperators tests AND, OR, NOT operators
func TestBooleanOperators(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "status", IsJSONB: false},
		{Name: "role", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "AND operator",
			query:   "name:john AND status:active",
			wantSQL: []string{"name = ?", "status = ?", "AND"},
		},
		{
			name:    "OR operator",
			query:   "name:john OR name:jane",
			wantSQL: []string{"name = ?", "OR"},
		},
		{
			name:    "NOT operator",
			query:   "name:john NOT status:inactive",
			wantSQL: []string{"name = ?", "status = ?", "NOT"},
		},
		{
			name:    "complex nested",
			query:   "(name:john OR name:jane) AND status:active",
			wantSQL: []string{"name = ?", "OR", "status = ?", "AND"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestEnhancedBooleanOperators tests &&, ||, ! operators
func TestEnhancedBooleanOperators(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "status", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "AND with &&",
			query:   "name:john && status:active",
			wantSQL: []string{"name = ?", "status = ?", "AND"},
		},
		{
			name:    "OR with ||",
			query:   "name:john || name:jane",
			wantSQL: []string{"name = ?", "OR"},
		},
		{
			name:    "NOT with !",
			query:   "name:john && !status:inactive",
			wantSQL: []string{"name = ?", "status = ?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestRequiredProhibited tests + and - operators
func TestRequiredProhibited(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "status", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "required term",
			query:   "+name:john",
			wantSQL: []string{"name = ?"},
		},
		{
			name:    "prohibited term",
			query:   "-status:inactive",
			wantSQL: []string{"NOT", "status = ?"},
		},
		{
			name:    "mixed required and prohibited",
			query:   "+name:john -status:inactive",
			wantSQL: []string{"name = ?", "NOT", "status = ?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestRangeQueries tests range queries with [] and {}
func TestRangeQueries(t *testing.T) {
	fields := []FieldInfo{
		{Name: "age", IsJSONB: false},
		{Name: "price", IsJSONB: false},
		{Name: "date", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "inclusive range",
			query:   "age:[18 TO 65]",
			wantSQL: []string{"age >= ?", "age <= ?"},
		},
		{
			name:    "exclusive range",
			query:   "age:{18 TO 65}",
			wantSQL: []string{"age > ?", "age < ?"},
		},
		{
			name:    "open-ended range min",
			query:   "age:[18 TO *]",
			wantSQL: []string{"age >= ?"},
		},
		{
			name:    "open-ended range max",
			query:   "age:[* TO 65]",
			wantSQL: []string{"age <= ?"},
		},
		{
			name:    "date range",
			query:   "date:[2024-01-01 TO 2024-12-31]",
			wantSQL: []string{"date >= ?", "date <= ?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
			t.Logf("SQL: %s, Values: %v", sql, vals)
		})
	}
}

// TestQuotedPhrases tests quoted phrase searches
func TestQuotedPhrases(t *testing.T) {
	fields := []FieldInfo{
		{Name: "description", IsJSONB: false},
		{Name: "title", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL string
	}{
		{
			name:    "simple quoted phrase",
			query:   `description:"hello world"`,
			wantSQL: "description = ?",
		},
		{
			name:    "phrase with special chars",
			query:   `title:"test: value"`,
			wantSQL: "title = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			if !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, tt.wantSQL)
			}
			t.Logf("SQL: %s, Values: %v", sql, vals)
		})
	}
}

// TestEscapedCharacters tests escaping special characters
func TestEscapedCharacters(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name      string
		query     string
		wantValue string
	}{
		{
			name:      "escaped colon",
			query:     `name:test\:value`,
			wantValue: "test:value",
		},
		{
			name:      "escaped plus",
			query:     `name:test\+value`,
			wantValue: "test+value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			if len(vals) > 0 {
				if val, ok := vals[0].(string); ok {
					if !strings.Contains(val, tt.wantValue) {
						t.Errorf("Expected value to contain %v, got %v", tt.wantValue, val)
					}
				}
			}
			t.Logf("SQL: %s, Values: %v", sql, vals)
		})
	}
}

// TestComplexQueries tests real-world complex queries
func TestComplexQueries(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "age", IsJSONB: false},
		{Name: "status", IsJSONB: false},
		{Name: "email", IsJSONB: false},
		{Name: "created_at", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "complex with ranges and wildcards",
			query: `name:john* AND age:[18 TO 65] AND status:active`,
		},
		{
			name:  "complex with required and prohibited",
			query: `+name:john -status:inactive age:[* TO 50]`,
		},
		{
			name:  "complex with alternative operators",
			query: `(name:john || name:jane) && status:active && age:{18 TO 65}`,
		},
		{
			name:  "complex with quoted phrases",
			query: `name:"John Doe" AND created_at:[2024-01-01 TO *]`,
		},
		{
			name:  "complex nested query",
			query: `(name:john* OR email:*@example.com) AND (status:active OR status:pending) AND age:[25 TO *]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			t.Logf("Query: %s", tt.query)
			t.Logf("SQL: %s", sql)
			t.Logf("Values: %v", vals)

			// Basic validation
			if sql == "" {
				t.Error("Expected non-empty SQL")
			}
		})
	}
}

// TestImplicitSearch tests implicit search across default fields
func TestImplicitSearch(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "email", IsJSONB: false},
		{Name: "description", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "implicit search",
			query:   "john",
			wantSQL: []string{"name", "email", "description", "OR"},
		},
		{
			name:    "implicit search with wildcard",
			query:   "john*",
			wantSQL: []string{"name", "email", "description", "OR"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
			t.Logf("SQL: %s, Values: %v", sql, vals)
		})
	}
}

// TestJSONBFields tests JSONB field access
func TestJSONBFields(t *testing.T) {
	fields := []FieldInfo{
		{Name: "metadata", IsJSONB: true},
		{Name: "name", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL string
	}{
		{
			name:    "JSONB field access",
			query:   "metadata.key:value",
			wantSQL: "metadata->>'key'",
		},
		{
			name:    "JSONB with wildcard",
			query:   "metadata.key:val*",
			wantSQL: "metadata->>'key' ILIKE ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			if !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, tt.wantSQL)
			}
			t.Logf("SQL: %s, Values: %v", sql, vals)
		})
	}
}

// TestMapOutput tests ParseToMap functionality
func TestMapOutput(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "status", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "simple term to map",
			query: "name:john",
		},
		{
			name:  "AND operator to map",
			query: "name:john AND status:active",
		},
		{
			name:  "range to map",
			query: "age:[18 TO 65]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ParseToMap(tt.query)
			if err != nil {
				t.Fatalf("ParseToMap() error = %v", err)
			}
			if result == nil {
				t.Error("Expected non-nil map result")
			}
			t.Logf("Map: %+v", result)
		})
	}
}

// TestLexer tests the lexer independently
func TestLexer(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantTokens []TokenType
	}{
		{
			name:  "simple field query",
			input: "name:john",
			wantTokens: []TokenType{
				TokenIdent, TokenColon, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "quoted string",
			input: `"hello world"`,
			wantTokens: []TokenType{
				TokenString, TokenEOF,
			},
		},
		{
			name:  "boolean operators",
			input: "name:john AND status:active",
			wantTokens: []TokenType{
				TokenIdent, TokenColon, TokenIdent, TokenAND, TokenIdent, TokenColon, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "alternative operators",
			input: "name:john && status:active",
			wantTokens: []TokenType{
				TokenIdent, TokenColon, TokenIdent, TokenAND, TokenIdent, TokenColon, TokenIdent, TokenEOF,
			},
		},
		{
			name:  "range query",
			input: "age:[18 TO 65]",
			wantTokens: []TokenType{
				TokenIdent, TokenColon, TokenLBracket, TokenNumber, TokenTO, TokenNumber, TokenRBracket, TokenEOF,
			},
		},
		{
			name:  "required and prohibited",
			input: "+name:john -status:inactive",
			wantTokens: []TokenType{
				TokenPlus, TokenIdent, TokenColon, TokenIdent, TokenMinus, TokenIdent, TokenColon, TokenIdent, TokenEOF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			var tokens []TokenType

			for {
				tok := lexer.NextToken()
				tokens = append(tokens, tok.Type)
				if tok.Type == TokenEOF || tok.Type == TokenError {
					break
				}
			}

			if len(tokens) != len(tt.wantTokens) {
				t.Errorf("Token count = %v, want %v", len(tokens), len(tt.wantTokens))
				t.Logf("Got tokens: %v", tokens)
				t.Logf("Want tokens: %v", tt.wantTokens)
				return
			}

			for i, tok := range tokens {
				if tok != tt.wantTokens[i] {
					t.Errorf("Token[%d] = %v, want %v", i, tok, tt.wantTokens[i])
				}
			}
		})
	}
}

// BenchmarkParser benchmarks the parser performance
func BenchmarkParser(b *testing.B) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "age", IsJSONB: false},
		{Name: "status", IsJSONB: false},
		{Name: "email", IsJSONB: false},
	}
	parser := NewParser(fields)

	query := `(name:john* OR email:*@example.com) AND (status:active OR status:pending) AND age:[25 TO 65]`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = parser.ParseToSQL(query)
	}
}
