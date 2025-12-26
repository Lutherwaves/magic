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
			wantSQL:  `"name" = $1`,
			wantVals: 1,
		},
		{
			name:     "wildcard prefix",
			query:    "name:john*",
			wantSQL:  `"name"::text ILIKE $1`,
			wantVals: 1,
		},
		{
			name:     "wildcard suffix",
			query:    "name:*john",
			wantSQL:  `"name"::text ILIKE $1`,
			wantVals: 1,
		},
		{
			name:     "wildcard contains",
			query:    "name:*john*",
			wantSQL:  `"name"::text ILIKE $1`,
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
			wantSQL: []string{`"name"`, `"status"`, "AND"},
		},
		{
			name:    "OR operator",
			query:   "name:john OR name:jane",
			wantSQL: []string{`"name"`, "OR"},
		},
		{
			name:    "NOT operator",
			query:   "name:john NOT status:inactive",
			wantSQL: []string{`"name"`, `"status"`, "NOT"},
		},
		{
			name:    "complex nested",
			query:   "(name:john OR name:jane) AND status:active",
			wantSQL: []string{`"name"`, `"status"`, "OR", "AND"},
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
			wantSQL: []string{`"name"`},
		},
		{
			name:    "prohibited term",
			query:   "-status:inactive",
			wantSQL: []string{`"status"`, "NOT"},
		},
		{
			name:    "mixed required and prohibited",
			query:   "+name:john -status:inactive",
			wantSQL: []string{`"name"`, `"status"`, "NOT"},
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

// TestRangeQueries tests range query syntax
func TestRangeQueries(t *testing.T) {
	fields := []FieldInfo{
		{Name: "age", IsJSONB: false},
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
			wantSQL: []string{`"age" BETWEEN`},
		},
		{
			name:    "exclusive range",
			query:   "age:{18 TO 65}",
			wantSQL: []string{`"age" >`, `"age" <`},
		},
		{
			name:    "open-ended range min",
			query:   "age:[18 TO *]",
			wantSQL: []string{`"age" >=`},
		},
		{
			name:    "open-ended range max",
			query:   "age:[* TO 65]",
			wantSQL: []string{`"age" <=`},
		},
		{
			name:    "date range",
			query:   "date:[2020-01-01 TO 2023-12-31]",
			wantSQL: []string{`"date"`},
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

// TestQuotedPhrases tests quoted phrase handling
func TestQuotedPhrases(t *testing.T) {
	fields := []FieldInfo{
		{Name: "description", IsJSONB: false},
		{Name: "title", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "simple quoted phrase",
			query:   `description:"hello world"`,
			wantSQL: []string{`"description"`},
		},
		{
			name:    "phrase with special chars",
			query:   `title:"Go: The Complete Guide"`,
			wantSQL: []string{`"title"`},
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

// TestEscapedCharacters tests escaped character handling
func TestEscapedCharacters(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "escaped colon",
			query:   `name:test\:value`,
			wantSQL: []string{`"name"`},
		},
		{
			name:    "escaped plus",
			query:   `name:C\+\+`,
			wantSQL: []string{`"name"`},
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

// TestComplexQueries tests complex query combinations
func TestComplexQueries(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "age", IsJSONB: false},
		{Name: "status", IsJSONB: false},
		{Name: "email", IsJSONB: false},
	}
	parser := NewParser(fields)

	tests := []struct {
		name      string
		query     string
		wantSQL   []string
		shouldErr bool
	}{
		{
			name:      "complex with ranges and wildcards",
			query:     "name:john* AND age:[25 TO 65]",
			wantSQL:   []string{`"name"`, `"age"`},
			shouldErr: false,
		},
		{
			name:      "complex with required and prohibited",
			query:     "+name:john -status:inactive AND age:[30 TO *]",
			wantSQL:   []string{`"name"`, `"status"`, `"age"`},
			shouldErr: false,
		},
		{
			name:      "complex with quoted phrases",
			query:     `name:"John Doe" AND (status:active OR status:pending)`,
			wantSQL:   []string{`"name"`, `"status"`},
			shouldErr: false,
		},
		{
			name:      "complex nested query",
			query:     "((name:john OR name:jane) AND status:active) OR (age:[18 TO 25] AND status:pending)",
			wantSQL:   []string{`"name"`, `"status"`, `"age"`},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("ParseToSQL() expected error but got none")
				}
				return
			}
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

// TestImplicitSearch tests implicit search across default fields
func TestImplicitSearch(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false, IsDefault: true},
		{Name: "email", IsJSONB: false, IsDefault: true},
		{Name: "description", IsJSONB: false, IsDefault: true},
	}
	parser := NewParser(fields)

	tests := []struct {
		name       string
		query      string
		wantOR     bool
		wantParams int
	}{
		{
			name:       "implicit search",
			query:      "john",
			wantOR:     true,
			wantParams: 3, // Should expand to 3 fields
		},
		{
			name:       "implicit search with wildcard",
			query:      "john*",
			wantOR:     true,
			wantParams: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			if tt.wantOR && !strings.Contains(sql, "OR") {
				t.Errorf("ParseToSQL() sql = %v, want to contain OR", sql)
			}
			if len(params) != tt.wantParams {
				t.Errorf("ParseToSQL() params count = %v, want %v", len(params), tt.wantParams)
			}
		})
	}
}

// TestJSONBFields tests JSONB field notation
func TestJSONBFields(t *testing.T) {
	fields := []FieldInfo{
		{Name: "metadata", IsJSONB: true},
	}
	parser := NewParser(fields)

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "JSONB field access",
			query:   "metadata.key:value",
			wantSQL: []string{`metadata->>'key'`},
		},
		{
			name:    "JSONB with wildcard",
			query:   "metadata.tags:prod*",
			wantSQL: []string{`metadata->>'tags'`, "ILIKE"},
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

// TestMapOutput tests the legacy map output format
func TestMapOutput(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", IsJSONB: false},
		{Name: "status", IsJSONB: false},
	}
	parser := NewParser(fields)

	result, err := parser.ParseToMap("name:john AND status:active")
	if err != nil {
		t.Fatalf("ParseToMap() error = %v", err)
	}

	if result == nil {
		t.Errorf("ParseToMap() returned nil")
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
