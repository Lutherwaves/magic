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

	// RenderFNs map - we handle most operators in renderParamInternal
	// Only keeping base implementations for operators we don't intercept
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
		expr.Like:      driver.Shared[expr.Like],
		expr.Greater:   driver.Shared[expr.Greater],
		expr.GreaterEq: driver.Shared[expr.GreaterEq],
		expr.Less:      driver.Shared[expr.Less],
		expr.LessEq:    driver.Shared[expr.LessEq],
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

	// Use our custom rendering logic
	str, params, err := p.renderParamInternal(e)
	if err != nil {
		return "", nil, err
	}

	// Convert ? to $N format
	str = convertToPostgresPlaceholders(str)

	return str, params, nil
}

// renderParamInternal dispatches to specialized renderers based on operator type.
func (p *PostgresJSONBDriver) renderParamInternal(e *expr.Expression) (string, []any, error) {
	if e == nil {
		return "", nil, nil
	}

	switch e.Op {
	case expr.Like, expr.Wild:
		return p.renderLikeOrWild(e)
	case expr.Fuzzy:
		return p.renderFuzzy(e)
	case expr.Boost:
		return "", nil, fmt.Errorf("boost operator (^) is not supported in SQL filtering; it only affects ranking/scoring")
	case expr.Range:
		return p.renderRange(e)
	case expr.Equals, expr.Greater, expr.Less, expr.GreaterEq, expr.LessEq:
		return p.renderComparison(e)
	case expr.And, expr.Or, expr.Must, expr.MustNot:
		return p.renderBinary(e)
	default:
		// Use base implementation for all other operators
		return p.Base.RenderParam(e)
	}
}

// renderLikeOrWild handles LIKE and Wild operators, converting them to PostgreSQL ILIKE.
func (p *PostgresJSONBDriver) renderLikeOrWild(e *expr.Expression) (string, []any, error) {
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
	if isJSONBSyntax(leftStr) {
		return fmt.Sprintf("%s ILIKE %s", leftStr, rightStr), params, nil
	}
	return fmt.Sprintf("%s::text ILIKE %s", leftStr, rightStr), params, nil
}

// renderFuzzy handles fuzzy search using PostgreSQL similarity() function.
// Requires pg_trgm extension.
// For queries like "name:roam~2", the structure is:
// - Op: Fuzzy
// - Left: Equals expression (name:roam) with Left=Column("name"), Right=Literal("roam")
// - Right: nil (distance stored in unexported fuzzyDistance field)
func (p *PostgresJSONBDriver) renderFuzzy(e *expr.Expression) (string, []any, error) {
	// Left side must be an Equals expression (field:value)
	leftExpr, ok := e.Left.(*expr.Expression)
	if !ok || leftExpr.Op != expr.Equals {
		return "", nil, fmt.Errorf("fuzzy operator requires field:value syntax (e.g., name:roam~2)")
	}

	// Extract column from the Equals expression's left side
	colStr, colParams, err := p.serializeColumn(leftExpr.Left)
	if err != nil {
		return "", nil, err
	}

	// Extract search term from the Equals expression's right side
	termStr, termParams, err := p.serializeValue(leftExpr.Right)
	if err != nil {
		return "", nil, err
	}

	params := append(colParams, termParams...)

	// Use similarity threshold of 0.3 (default for fuzzy search)
	// Lower threshold = more matches, higher = stricter matching
	// The fuzzy distance from go-lucene is not directly accessible (unexported),
	// so we use a reasonable default threshold
	threshold := 0.3

	// For JSONB fields, we need to cast to text for similarity
	if isJSONBSyntax(colStr) {
		return fmt.Sprintf("similarity(%s, %s) > %f", colStr, termStr, threshold), params, nil
	}
	return fmt.Sprintf("similarity(%s::text, %s) > %f", colStr, termStr, threshold), params, nil
}

// renderComparison handles comparison operators (=, >, <, >=, <=) with nil/null support.
func (p *PostgresJSONBDriver) renderComparison(e *expr.Expression) (string, []any, error) {
	// Get the left side (column name)
	leftStr, leftParams, err := p.serializeColumn(e.Left)
	if err != nil {
		return "", nil, err
	}

	// Check if right side is nil/null for IS NULL handling
	if isNilValue(e.Right) {
		if e.Op == expr.Equals {
			return fmt.Sprintf("%s IS NULL", leftStr), leftParams, nil
		}
		return "", nil, fmt.Errorf("cannot use comparison operators (>, <, >=, <=) with nil value")
	}

	// Get the right side value
	rightStr, rightParams, err := p.serializeValue(e.Right)
	if err != nil {
		return "", nil, err
	}

	params := append(leftParams, rightParams...)

	// Determine the operator symbol
	var opSymbol string
	switch e.Op {
	case expr.Equals:
		opSymbol = "="
	case expr.Greater:
		opSymbol = ">"
	case expr.Less:
		opSymbol = "<"
	case expr.GreaterEq:
		opSymbol = ">="
	case expr.LessEq:
		opSymbol = "<="
	}

	return fmt.Sprintf("%s %s %s", leftStr, opSymbol, rightStr), params, nil
}

// renderBinary handles binary operators (AND, OR, Must, MustNot) with recursive rendering.
// Note: Must and MustNot are unary operators (only Right operand), while And and Or are binary.
func (p *PostgresJSONBDriver) renderBinary(e *expr.Expression) (string, []any, error) {
	switch e.Op {
	case expr.Must, expr.MustNot:
		// Unary operators: operand is in Left (not Right)
		if e.Left == nil {
			return "", nil, fmt.Errorf("%s operator requires a left operand", e.Op)
		}

		// Try to render Left as an expression first
		if leftExpr, ok := e.Left.(*expr.Expression); ok {
			leftStr, leftParams, err := p.renderParamInternal(leftExpr)
			if err != nil {
				return "", nil, err
			}

			if e.Op == expr.Must {
				return leftStr, leftParams, nil
			}
			// MustNot
			return fmt.Sprintf("NOT (%s)", leftStr), leftParams, nil
		}

		// If Left is not an expression, try to render it directly
		// This handles cases where Left might be a Column, Literal, etc.
		leftStr, leftParams, err := p.serializeColumn(e.Left)
		if err != nil {
			// Try as a value if column serialization fails
			leftStr, leftParams, err = p.serializeValue(e.Left)
			if err != nil {
				// Fallback to base implementation if we can't serialize
				return p.Base.RenderParam(e)
			}
		}

		if e.Op == expr.Must {
			return leftStr, leftParams, nil
		}
		// MustNot
		return fmt.Sprintf("NOT (%s)", leftStr), leftParams, nil

	case expr.And, expr.Or:
		// Binary operators: both Left and Right operands are required
		if e.Left == nil || e.Right == nil {
			return "", nil, fmt.Errorf("%s operator requires both left and right operands", e.Op)
		}

		leftExpr, leftIsExpr := e.Left.(*expr.Expression)
		rightExpr, rightIsExpr := e.Right.(*expr.Expression)

		if !leftIsExpr || !rightIsExpr {
			// Fallback to base implementation if operands aren't expressions
			return p.Base.RenderParam(e)
		}

		leftStr, leftParams, err := p.renderParamInternal(leftExpr)
		if err != nil {
			return "", nil, err
		}

		rightStr, rightParams, err := p.renderParamInternal(rightExpr)
		if err != nil {
			return "", nil, err
		}

		params := append(leftParams, rightParams...)

		if e.Op == expr.And {
			return fmt.Sprintf("(%s) AND (%s)", leftStr, rightStr), params, nil
		}
		// Or
		return fmt.Sprintf("(%s) OR (%s)", leftStr, rightStr), params, nil

	default:
		return "", nil, fmt.Errorf("unsupported operator: %v", e.Op)
	}
}

// serializeColumn serializes a column reference.
func (p *PostgresJSONBDriver) serializeColumn(in any) (string, []any, error) {
	switch v := in.(type) {
	case expr.Column:
		colStr := string(v)
		// Don't quote JSONB syntax (contains ->>)
		if isJSONBSyntax(colStr) {
			return colStr, nil, nil
		}
		return fmt.Sprintf(`"%s"`, colStr), nil, nil
	case string:
		// Handle string columns (for some operators)
		if isJSONBSyntax(v) {
			return v, nil, nil
		}
		return fmt.Sprintf(`"%s"`, v), nil, nil
	case *expr.Expression:
		// Handle LITERAL(COLUMN(...)) pattern
		if v.Op == expr.Literal && v.Left != nil {
			if col, ok := v.Left.(expr.Column); ok {
				colStr := string(col)
				// Don't quote JSONB syntax
				if isJSONBSyntax(colStr) {
					return colStr, nil, nil
				}
				return fmt.Sprintf(`"%s"`, colStr), nil, nil
			}
		}
		// For other expressions, recursively render using our custom logic
		return p.renderParamInternal(v)
	default:
		return "", nil, fmt.Errorf("unexpected column type: %T", v)
	}
}

// serializeValue serializes a value with wildcard conversion.
func (p *PostgresJSONBDriver) serializeValue(in any) (string, []any, error) {
	switch v := in.(type) {
	case string:
		return "?", []any{convertWildcards(v)}, nil
	case *expr.Expression:
		// For LITERAL expressions, extract the value and convert wildcards
		if v.Op == expr.Literal && v.Left != nil {
			literalVal := fmt.Sprintf("%v", v.Left)
			return "?", []any{convertWildcards(literalVal)}, nil
		}
		// For WILD expressions, extract the pattern from Left
		if v.Op == expr.Wild && v.Left != nil {
			literalVal := fmt.Sprintf("%v", v.Left)
			return "?", []any{convertWildcards(literalVal)}, nil
		}
		// For nested expressions, recursively render
		return p.renderParamInternal(v)
	case nil:
		// Handle nil value - this can happen with malformed expressions
		return "", nil, fmt.Errorf("nil value in expression")
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

// Helper functions for DRY and cleaner code

// convertWildcards converts Lucene wildcards to SQL wildcards.
// * (any characters) → % (SQL wildcard)
// ? (single character) → _ (SQL wildcard)
//
// Note: go-lucene's base driver also converts wildcards, but only for expr.Like operators.
// We need this function because we also convert wildcards for expr.Wild expressions
// and when serializing values for fuzzy search and other operators.
func convertWildcards(s string) string {
	// Use a builder for efficient string manipulation
	var result strings.Builder
	result.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '*':
			result.WriteByte('%')
		case '?':
			result.WriteByte('_')
		default:
			result.WriteByte(c)
		}
	}
	return result.String()
}

// isJSONBSyntax checks if a column string contains JSONB accessor syntax.
func isJSONBSyntax(col string) bool {
	return strings.Contains(col, "->>")
}

// isNilValue checks if a value represents nil/null in Lucene query syntax.
// Supports: nil, null, NULL (case-insensitive)
// Note: We intentionally do NOT support "empty" as it could be a legitimate search value.
func isNilValue(v any) bool {
	strVal := extractStringValue(v)
	if strVal == "" {
		return false
	}
	lower := strings.ToLower(strVal)
	return lower == "nil" || lower == "null"
}

// extractStringValue extracts a string value from various expression types.
func extractStringValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case *expr.Expression:
		if val.Op == expr.Literal && val.Left != nil {
			if strVal, ok := val.Left.(string); ok {
				return strVal
			}
		}
	}
	return ""
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
	str, params, err := d.RenderParam(e)
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
