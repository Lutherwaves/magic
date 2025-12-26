package lucene

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	lucene "github.com/grindlemire/go-lucene"
	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// Safety limits for query parsing (OWASP A04: Insecure Design - DoS prevention)
const (
	DefaultMaxQueryLength = 10000 // 10KB - prevents memory exhaustion
	DefaultMaxDepth       = 20    // Prevents stack overflow from deep nesting
	DefaultMaxTerms       = 100   // Prevents CPU exhaustion from complex queries
)

// FieldInfo describes a searchable field and its properties.
type FieldInfo struct {
	Name      string
	IsJSONB   bool
	IsDefault bool // Whether this field is searched in implicit queries (no field prefix)
}

// Parser provides Lucene query parsing with security limits.
type Parser struct {
	DefaultFields []FieldInfo

	// Security limits (configurable with safe defaults)
	MaxQueryLength int // Maximum query string length (default: 10KB)
	MaxDepth       int // Maximum nesting depth (default: 20)
	MaxTerms       int // Maximum number of terms (default: 100)

	// Custom drivers for different backends
	postgresDriver *PostgresJSONBDriver
	dynamoDriver   *DynamoDBPartiQLDriver
}

// NewParserFromType creates a parser by introspecting a struct's fields.
// This is the recommended approach for initializing parsers as it:
// - Works with any backend (PostgreSQL, MySQL, DynamoDB, etc.)
// - Zero database overhead
// - Compile-time safety
// - Auto-detects JSONB fields from gorm tags
// - Auto-sets string fields as searchable (IsDefault=true)
//
// Example:
//
//	type Task struct {
//	    ID          string    `json:"id"`
//	    Name        string    `json:"name"`                         // Auto: IsDefault=true
//	    Description string    `json:"description"`                  // Auto: IsDefault=true
//	    Status      string    `json:"status" lucene:"nodefault"`    // Explicit: IsDefault=false
//	    CreatedAt   time.Time `json:"created_at"`                   // Auto: IsDefault=false (not string)
//	    Labels      JSONB     `json:"labels" gorm:"type:jsonb"`     // Auto: IsJSONB=true, IsDefault=false
//	}
//
//	parser, err := lucene.NewParserFromType(Task{})
//
// Struct tag controls:
// - lucene:"default"   - Force IsDefault=true (include in implicit search)
// - lucene:"nodefault" - Force IsDefault=false (require explicit field prefix)
// - gorm:"type:jsonb"  - Auto-detected as JSONB field
//
// Auto-detection rules (when no lucene tag):
// - String fields: IsDefault=true (searchable in implicit queries)
// - Non-string fields (int, time.Time, uuid, etc.): IsDefault=false
// - JSONB fields: IsDefault=false (require field.subfield syntax)
func NewParserFromType(model any) (*Parser, error) {
	fields, err := getStructFields(model)
	if err != nil {
		return nil, err
	}
	return NewParser(fields), nil
}

// NewParser creates a new Lucene query parser with the given default fields.
func NewParser(defaultFields []FieldInfo) *Parser {
	return &Parser{
		DefaultFields:  defaultFields,
		MaxQueryLength: DefaultMaxQueryLength,
		MaxDepth:       DefaultMaxDepth,
		MaxTerms:       DefaultMaxTerms,
		postgresDriver: NewPostgresJSONBDriver(defaultFields),
		dynamoDriver:   NewDynamoDBPartiQLDriver(defaultFields),
	}
}

// getStructFields extracts field information from a struct using reflection.
// It sets IsDefault=true for string fields (text-like) and IsDefault=false for
// other types (int, time, uuid, etc.) following the same logic as schema introspection.
func getStructFields(model any) ([]FieldInfo, error) {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", t.Kind())
	}

	var fields []FieldInfo
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		if commaIdx := strings.Index(jsonTag, ","); commaIdx != -1 {
			jsonTag = jsonTag[:commaIdx]
		}

		gormTag := field.Tag.Get("gorm")
		isJSONB := strings.Contains(gormTag, "type:jsonb")

		// Check if the lucene tag explicitly sets isDefault
		luceneTag := field.Tag.Get("lucene")
		isDefault := false
		if luceneTag == "default" {
			isDefault = true
		} else if luceneTag != "nodefault" {
			// Auto-detect: string types are default, others are not
			isDefault = field.Type.Kind() == reflect.String && !isJSONB
		}

		fields = append(fields, FieldInfo{
			Name:      jsonTag,
			IsJSONB:   isJSONB,
			IsDefault: isDefault,
		})
	}

	return fields, nil
}

// ParseToMap parses a Lucene query into a map representation.
// Note: This is a legacy method kept for backward compatibility.
func (p *Parser) ParseToMap(query string) (map[string]any, error) {
	// Security: Validate query length (OWASP A04: DoS prevention)
	if err := p.validateQuery(query); err != nil {
		return nil, err
	}

	// Parse using the library
	e, err := p.parseWithDefaults(query)
	if err != nil {
		return nil, err
	}

	// Convert expression to map
	return p.exprToMap(e), nil
}

// ParseToSQL parses a Lucene query and converts it to PostgreSQL SQL with parameters.
func (p *Parser) ParseToSQL(query string) (string, []any, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to SQL: %s`, query))

	// Security: Validate query length (OWASP A04: DoS prevention)
	if err := p.validateQuery(query); err != nil {
		return "", nil, err
	}

	// Parse using the library
	e, err := p.parseWithDefaults(query)
	if err != nil {
		return "", nil, err
	}

	// Render using custom PostgreSQL driver
	sql, params, err := p.postgresDriver.RenderParam(e)
	if err != nil {
		return "", nil, err
	}

	return sql, params, nil
}

// ParseToDynamoDBPartiQL parses a Lucene query and converts it to DynamoDB PartiQL.
func (p *Parser) ParseToDynamoDBPartiQL(query string) (string, []types.AttributeValue, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to DynamoDB PartiQL: %s`, query))

	// Security: Validate query length (OWASP A04: DoS prevention)
	if err := p.validateQuery(query); err != nil {
		return "", nil, err
	}

	// Parse using the library
	e, err := p.parseWithDefaults(query)
	if err != nil {
		return "", nil, err
	}

	// Render using custom DynamoDB driver
	partiql, attrs, err := p.dynamoDriver.RenderPartiQL(e)
	if err != nil {
		return "", nil, err
	}

	return partiql, attrs, nil
}

// validateQuery checks security limits.
func (p *Parser) validateQuery(query string) error {
	if len(query) > p.MaxQueryLength {
		return fmt.Errorf("query too long: %d bytes exceeds maximum of %d bytes", len(query), p.MaxQueryLength)
	}
	return nil
}

// parseWithDefaults parses a query with multi-field default support.
// If the query contains unfielded terms, it expands them across all default fields with OR.
func (p *Parser) parseWithDefaults(query string) (*expr.Expression, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Check if query has any explicit field:value patterns
	hasExplicitFields := strings.Contains(query, ":")

	// Get fields marked for implicit search (IsDefault = true)
	var implicitSearchFields []FieldInfo
	for _, field := range p.DefaultFields {
		if field.IsDefault {
			implicitSearchFields = append(implicitSearchFields, field)
		}
	}

	// If there are explicit fields, just parse normally with the first default field as fallback
	if hasExplicitFields {
		defaultField := ""
		if len(implicitSearchFields) > 0 {
			defaultField = implicitSearchFields[0].Name
		} else if len(p.DefaultFields) > 0 {
			defaultField = p.DefaultFields[0].Name
		}
		return lucene.Parse(query, lucene.WithDefaultField(defaultField))
	}

	// Query has no explicit fields - create OR across all IsDefault=true fields
	if len(implicitSearchFields) == 0 {
		return nil, fmt.Errorf("no default fields configured for implicit search")
	}

	// For simple queries without fields, expand to: (field1:query OR field2:query OR ...)
	var orExpressions []*expr.Expression
	for _, field := range implicitSearchFields {
		// Parse with this specific field as default
		e, err := lucene.Parse(query, lucene.WithDefaultField(field.Name))
		if err != nil {
			return nil, err
		}
		orExpressions = append(orExpressions, e)
	}

	// Combine all expressions with OR
	if len(orExpressions) == 1 {
		return orExpressions[0], nil
	}

	// Build OR tree
	result := orExpressions[0]
	for i := 1; i < len(orExpressions); i++ {
		result = expr.Expr(result, expr.Or, orExpressions[i])
	}

	return result, nil
}

// exprToMap converts an expression to a map representation (legacy format).
func (p *Parser) exprToMap(e *expr.Expression) map[string]any {
	if e == nil {
		return nil
	}

	result := make(map[string]any)

	switch e.Op {
	case expr.Equals:
		if col, ok := e.Left.(expr.Column); ok {
			result[string(col)] = p.valueToAny(e.Right)
		}
	case expr.Like:
		if col, ok := e.Left.(expr.Column); ok {
			pattern := p.valueToAny(e.Right)
			result[string(col)] = map[string]any{"$like": pattern}
		}
	case expr.And, expr.Or, expr.Not:
		var children []map[string]any
		if leftExpr, ok := e.Left.(*expr.Expression); ok {
			children = append(children, p.exprToMap(leftExpr))
		}
		if rightExpr, ok := e.Right.(*expr.Expression); ok {
			children = append(children, p.exprToMap(rightExpr))
		}
		result[e.Op.String()] = children
	default:
		// For other operators, do a simple conversion
		if col, ok := e.Left.(expr.Column); ok {
			result[string(col)] = p.valueToAny(e.Right)
		}
	}

	return result
}

// valueToAny converts expression values to any type.
func (p *Parser) valueToAny(v any) any {
	switch val := v.(type) {
	case *expr.Expression:
		return p.exprToMap(val)
	case string:
		return val
	case int, float64:
		return val
	default:
		return fmt.Sprintf("%v", v)
	}
}
