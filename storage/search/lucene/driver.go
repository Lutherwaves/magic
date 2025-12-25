package lucene

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/grindlemire/go-lucene/pkg/driver"
	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// PostgresJSONBDriver is a custom PostgreSQL driver that supports JSONB field notation.
// It extends the base PostgreSQL driver to handle field->>'subfield' syntax.
type PostgresJSONBDriver struct {
	driver.Base
	fields map[string]FieldInfo // Map of field names to their metadata
}

// NewPostgresJSONBDriver creates a new PostgreSQL driver with JSONB support.
func NewPostgresJSONBDriver(fields []FieldInfo) *PostgresJSONBDriver {
	fieldMap := make(map[string]FieldInfo)
	for _, f := range fields {
		fieldMap[f.Name] = f
	}

	fns := map[expr.Operator]driver.RenderFN{
		expr.Literal:   driver.Shared[expr.Literal],
		expr.And:       driver.Shared[expr.And],
		expr.Or:        driver.Shared[expr.Or],
		expr.Not:       driver.Shared[expr.Not],
		expr.Equals:    customPostgresEquals,    // Custom to handle JSONB syntax
		expr.Range:     driver.Shared[expr.Range], // Handled in renderParamInternal
		expr.Must:      driver.Shared[expr.Must],
		expr.MustNot:   driver.Shared[expr.MustNot],
		expr.Wild:      customPostgresWild,      // Custom to use ILIKE instead of SIMILAR TO
		expr.Regexp:    driver.Shared[expr.Regexp],
		expr.Like:      customPostgresLike,      // Custom LIKE to use ILIKE
		expr.Greater:   customPostgresComparison(">"),
		expr.GreaterEq: customPostgresComparison(">="),
		expr.Less:      customPostgresComparison("<"),
		expr.LessEq:    customPostgresComparison("<="),
		expr.In:        driver.Shared[expr.In],
		expr.List:      driver.Shared[expr.List],
	}

	return &PostgresJSONBDriver{
		Base: driver.Base{
			RenderFNs: fns,
		},
		fields: fieldMap,
	}
}

// RenderParam renders the expression with PostgreSQL-style $N placeholders.
func (p *PostgresJSONBDriver) RenderParam(e *expr.Expression) (string, []any, error) {
	// Process JSONB field notation before rendering
	p.processJSONBFields(e)

	// Use base rendering with ? placeholders
	str, params, err := p.renderParamInternal(e)
	if err != nil {
		return "", nil, err
	}

	// Convert ? to $N format
	str = convertToPostgresPlaceholders(str)

	return str, params, nil
}

// renderParamInternal handles rendering with special ILIKE and Range logic.
func (p *PostgresJSONBDriver) renderParamInternal(e *expr.Expression) (string, []any, error) {
	if e == nil {
		return "", nil, nil
	}

	// Special handling for LIKE operator - convert to ILIKE
	if e.Op == expr.Like {
		// Get the left side (column name)
		leftStr, leftParams, err := p.serializeColumn(e.Left)
		if err != nil {
			return "", nil, err
		}

		// Get the right side value (the pattern)
		rightStr, rightParams, err := p.serializeValue(e.Right)
		if err != nil {
			return "", nil, err
		}

		params := append(leftParams, rightParams...)

		// Check if left contains JSONB syntax
		if strings.Contains(leftStr, "->>") {
			return fmt.Sprintf("%s ILIKE %s", leftStr, rightStr), params, nil
		}
		return fmt.Sprintf("%s::text ILIKE %s", leftStr, rightStr), params, nil
	}

	// Special handling for Range operator - handle open-ended ranges with *
	if e.Op == expr.Range {
		return p.renderRange(e)
	}

	// Use base implementation for all other operators
	return p.Base.RenderParam(e)
}

// serializeColumn serializes a column reference.
func (p *PostgresJSONBDriver) serializeColumn(in any) (string, []any, error) {
	switch v := in.(type) {
	case expr.Column:
		colStr := string(v)
		// Don't quote JSONB syntax (contains ->>)
		if strings.Contains(colStr, "->>") {
			return colStr, nil, nil
		}
		return fmt.Sprintf(`"%s"`, colStr), nil, nil
	case *expr.Expression:
		// Handle LITERAL(COLUMN(...)) pattern
		if v.Op == expr.Literal && v.Left != nil {
			if col, ok := v.Left.(expr.Column); ok {
				colStr := string(col)
				// Don't quote JSONB syntax
				if strings.Contains(colStr, "->>") {
					return colStr, nil, nil
				}
				return fmt.Sprintf(`"%s"`, colStr), nil, nil
			}
		}
		// For other expressions, use base renderer
		return p.Base.RenderParam(v)
	default:
		return "", nil, fmt.Errorf("unexpected column type: %T", v)
	}
}

// serializeValue serializes a value with wildcard conversion.
func (p *PostgresJSONBDriver) serializeValue(in any) (string, []any, error) {
	switch v := in.(type) {
	case string:
		// Convert Lucene wildcards to SQL wildcards
		val := strings.ReplaceAll(v, "*", "%")
		val = strings.ReplaceAll(val, "?", "_")
		return "?", []any{val}, nil
	case *expr.Expression:
		return p.Base.RenderParam(v)
	default:
		return "?", []any{v}, nil
	}
}

// processJSONBFields recursively processes the expression tree to convert
// field.subfield notation to PostgreSQL JSONB syntax field->>'subfield'.
func (p *PostgresJSONBDriver) processJSONBFields(e *expr.Expression) {
	if e == nil {
		return
	}

	// Process left side if it's a column
	if col, ok := e.Left.(expr.Column); ok {
		e.Left = p.formatFieldName(string(col))
	}

	// Recursively process expressions
	if leftExpr, ok := e.Left.(*expr.Expression); ok {
		p.processJSONBFields(leftExpr)
	}
	if rightExpr, ok := e.Right.(*expr.Expression); ok {
		p.processJSONBFields(rightExpr)
	}

	// Process expression slices
	if exprs, ok := e.Left.([]*expr.Expression); ok {
		for _, ex := range exprs {
			p.processJSONBFields(ex)
		}
	}
	if exprs, ok := e.Right.([]*expr.Expression); ok {
		for _, ex := range exprs {
			p.processJSONBFields(ex)
		}
	}
}

// formatFieldName converts field.subfield to JSONB syntax if the base field is JSONB.
func (p *PostgresJSONBDriver) formatFieldName(fieldName string) expr.Column {
	parts := strings.SplitN(fieldName, ".", 2)
	if len(parts) == 2 {
		baseField := parts[0]
		subField := parts[1]

		if field, exists := p.fields[baseField]; exists && field.IsJSONB {
			// Return as JSONB operator syntax
			return expr.Column(fmt.Sprintf("%s->>'%s'", baseField, subField))
		}
	}
	return expr.Column(fieldName)
}

// unquoteJSONB removes quotes from JSONB syntax to make it work in PostgreSQL.
// Converts: "labels->>'key'" to: labels->>'key'
func unquoteJSONB(col string) string {
	if strings.Contains(col, "->>") {
		// Remove only the outermost quotes if they exist
		if len(col) >= 2 && col[0] == '"' && col[len(col)-1] == '"' {
			return col[1 : len(col)-1]
		}
	}
	return col
}

// customPostgresEquals implements Equals operator with JSONB support.
func customPostgresEquals(left, right string) (string, error) {
	left = unquoteJSONB(left)
	return fmt.Sprintf("%s = %s", left, right), nil
}

// customPostgresComparison creates comparison operators (>, >=, <, <=) with JSONB support.
func customPostgresComparison(op string) driver.RenderFN {
	return func(left, right string) (string, error) {
		left = unquoteJSONB(left)
		return fmt.Sprintf("%s %s %s", left, op, right), nil
	}
}

// customPostgresLike implements case-insensitive LIKE using ILIKE.
func customPostgresLike(left, right string) (string, error) {
	// Unquote JSONB syntax first
	left = unquoteJSONB(left)

	// Check if it's a regex pattern /.../
	if len(right) >= 4 && right[1] == '/' && right[len(right)-2] == '/' {
		return fmt.Sprintf("%s ~ %s", left, right), nil
	}

	// Replace wildcards
	right = strings.ReplaceAll(right, "*", "%")
	right = strings.ReplaceAll(right, "?", "_")

	// Use ILIKE for case-insensitive matching
	// Also cast field to text if it's not already JSONB syntax
	if strings.Contains(left, "->>") {
		return fmt.Sprintf("%s ILIKE %s", left, right), nil
	}
	return fmt.Sprintf("%s::text ILIKE %s", left, right), nil
}

// customPostgresWild implements wildcard matching using ILIKE instead of SIMILAR TO.
func customPostgresWild(left, right string) (string, error) {
	// Delegate to LIKE handler which already handles wildcards correctly
	return customPostgresLike(left, right)
}

// extractLiteralValue extracts the literal value from an expression or returns it as-is.
func extractLiteralValue(v any) string {
	if v == nil {
		return ""
	}

	// If it's an expression, try to extract the Left value (for LITERAL expressions)
	if ex, ok := v.(*expr.Expression); ok {
		if ex.Op == expr.Literal && ex.Left != nil {
			// LITERAL expressions store the actual value in Left
			return fmt.Sprintf("%v", ex.Left)
		}
		// For other expression types, return the string representation
		return fmt.Sprintf("%v", v)
	}

	// For non-expression types, return as string
	return fmt.Sprintf("%v", v)
}

// renderRange handles range expressions with support for open-ended ranges (*).
func (p *PostgresJSONBDriver) renderRange(e *expr.Expression) (string, []any, error) {
	// Get column name
	colStr, _, err := p.serializeColumn(e.Left)
	if err != nil {
		return "", nil, err
	}

	// The Right side should be a RangeBoundary
	rangeBoundary, ok := e.Right.(*expr.RangeBoundary)
	if !ok {
		return "", nil, fmt.Errorf("invalid range expression structure: expected *expr.RangeBoundary, got %T", e.Right)
	}

	// Extract min and max values by rendering them
	var minVal, maxVal string
	var params []any

	// Extract Min value
	if rangeBoundary.Min != nil {
		minVal = extractLiteralValue(rangeBoundary.Min)
	}

	// Extract Max value
	if rangeBoundary.Max != nil {
		maxVal = extractLiteralValue(rangeBoundary.Max)
	}

	// Handle open-ended ranges
	if minVal == "*" && maxVal == "*" {
		return "", nil, fmt.Errorf("both range bounds cannot be wildcards")
	}

	if minVal == "*" {
		// [* TO max] or {* TO max}
		params = append(params, maxVal)
		if rangeBoundary.Inclusive {
			return fmt.Sprintf("%s <= ?", colStr), params, nil
		}
		return fmt.Sprintf("%s < ?", colStr), params, nil
	}

	if maxVal == "*" {
		// [min TO *] or {min TO *}
		params = append(params, minVal)
		if rangeBoundary.Inclusive {
			return fmt.Sprintf("%s >= ?", colStr), params, nil
		}
		return fmt.Sprintf("%s > ?", colStr), params, nil
	}

	// Both bounds specified
	params = append(params, minVal, maxVal)
	if rangeBoundary.Inclusive {
		return fmt.Sprintf("%s BETWEEN ? AND ?", colStr), params, nil
	}
	return fmt.Sprintf("(%s > ? AND %s < ?)", colStr, colStr), params, nil
}

// DynamoDBPartiQLDriver converts Lucene queries to DynamoDB PartiQL.
type DynamoDBPartiQLDriver struct {
	driver.Base
	fields map[string]FieldInfo
}

// NewDynamoDBPartiQLDriver creates a new DynamoDB PartiQL driver.
func NewDynamoDBPartiQLDriver(fields []FieldInfo) *DynamoDBPartiQLDriver {
	fieldMap := make(map[string]FieldInfo)
	for _, f := range fields {
		fieldMap[f.Name] = f
	}

	fns := map[expr.Operator]driver.RenderFN{
		expr.Literal:   driver.Shared[expr.Literal],
		expr.And:       driver.Shared[expr.And],
		expr.Or:        driver.Shared[expr.Or],
		expr.Not:       driver.Shared[expr.Not],
		expr.Equals:    driver.Shared[expr.Equals],
		expr.Range:     driver.Shared[expr.Range],
		expr.Must:      driver.Shared[expr.Must],
		expr.MustNot:   driver.Shared[expr.MustNot],
		expr.Wild:      driver.Shared[expr.Wild],
		expr.Regexp:    driver.Shared[expr.Regexp],
		expr.Like:      dynamoDBLike, // Custom LIKE for DynamoDB functions
		expr.Greater:   driver.Shared[expr.Greater],
		expr.GreaterEq: driver.Shared[expr.GreaterEq],
		expr.Less:      driver.Shared[expr.Less],
		expr.LessEq:    driver.Shared[expr.LessEq],
		expr.In:        driver.Shared[expr.In],
		expr.List:      driver.Shared[expr.List],
	}

	return &DynamoDBPartiQLDriver{
		Base: driver.Base{
			RenderFNs: fns,
		},
		fields: fieldMap,
	}
}

// RenderPartiQL renders the expression to DynamoDB PartiQL with AttributeValue parameters.
func (d *DynamoDBPartiQLDriver) RenderPartiQL(e *expr.Expression) (string, []types.AttributeValue, error) {
	// Use base rendering with ? placeholders
	str, params, err := d.Base.RenderParam(e)
	if err != nil {
		return "", nil, err
	}

	// Convert params to DynamoDB AttributeValues
	attrValues := make([]types.AttributeValue, len(params))
	for i, param := range params {
		attrValues[i] = &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", param)}
	}

	return str, attrValues, nil
}

// dynamoDBLike implements LIKE using DynamoDB's begins_with and contains functions.
func dynamoDBLike(left, right string) (string, error) {
	// Remove quotes from right side to analyze pattern
	pattern := strings.Trim(right, "'")

	// Replace wildcards for analysis
	hasPrefix := strings.HasPrefix(pattern, "%")
	hasSuffix := strings.HasSuffix(pattern, "%")

	if hasPrefix && hasSuffix {
		// %value% -> contains(field, value)
		value := strings.Trim(pattern, "%")
		return fmt.Sprintf("contains(%s, '%s')", left, value), nil
	} else if !hasPrefix && hasSuffix {
		// value% -> begins_with(field, value)
		value := strings.TrimSuffix(pattern, "%")
		return fmt.Sprintf("begins_with(%s, '%s')", left, value), nil
	} else if hasPrefix && !hasSuffix {
		// %value -> contains(field, value) (DynamoDB doesn't have ends_with)
		value := strings.TrimPrefix(pattern, "%")
		return fmt.Sprintf("contains(%s, '%s')", left, value), nil
	}

	// Exact match
	return fmt.Sprintf("%s = %s", left, right), nil
}

// convertToPostgresPlaceholders converts ? placeholders to PostgreSQL's $N format.
func convertToPostgresPlaceholders(query string) string {
	paramIndex := 1
	result := strings.Builder{}
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			result.WriteString(fmt.Sprintf("$%d", paramIndex))
			paramIndex++
		} else {
			result.WriteByte(query[i])
		}
	}
	return result.String()
}
