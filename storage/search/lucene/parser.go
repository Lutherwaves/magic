package lucene

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Safety limits for query parsing (OWASP A04: Insecure Design - DoS prevention)
const (
	DefaultMaxQueryLength = 10000 // 10KB - prevents memory exhaustion
	DefaultMaxDepth       = 20    // Prevents stack overflow from deep nesting
	DefaultMaxTerms       = 100   // Prevents CPU exhaustion from complex queries
)

type FieldInfo struct {
	Name    string
	IsJSONB bool
}

// RangeNode represents a range query [min TO max] or {min TO max}
type RangeNode struct {
	Field     string
	Min       string
	Max       string
	Inclusive bool // true for [], false for {}
}

// EnhancedNode extends Node with additional Lucene features
type EnhancedNode struct {
	*Node
	Required   bool    // + operator
	Prohibited bool    // - operator
	Boost      float64 // ^ operator
	Proximity  int     // ~n for phrases
	Fuzzy      int     // ~n for terms
	IsPhrase   bool    // quoted string
	RangeInfo  *RangeNode
}

type Parser struct {
	DefaultFields []FieldInfo

	// Security limits (configurable with safe defaults)
	MaxQueryLength int // Maximum query string length (default: 10KB)
	MaxDepth       int // Maximum nesting depth (default: 20)
	MaxTerms       int // Maximum number of terms (default: 100)

	// Internal tracking
	termCount int // Tracks number of terms during parsing

	// Lexer state
	lexer   *Lexer
	current Token
	peek    Token
}

type NodeType int

const (
	NodeTerm NodeType = iota
	NodeWildcard
	NodeLogical
	NodePhrase
	NodeRange
	NodeFuzzy
	NodeProximity
)

type LogicalOperator string

const (
	AND LogicalOperator = "AND"
	OR  LogicalOperator = "OR"
	NOT LogicalOperator = "NOT"
)

type MatchType int

const (
	matchExact MatchType = iota
	matchStartsWith
	matchEndsWith
	matchContains
)

type ComparisonOperator string

const (
	OpEqual              ComparisonOperator = "="
	OpGreaterThan        ComparisonOperator = ">"
	OpGreaterThanOrEqual ComparisonOperator = ">="
	OpLessThan           ComparisonOperator = "<"
	OpLessThanOrEqual    ComparisonOperator = "<="
)

type Node struct {
	Type       NodeType
	Field      string
	Value      string
	Operator   LogicalOperator
	Comparison ComparisonOperator // For range queries
	Children   []*Node
	Negate     bool
	MatchType  MatchType
}

func NewParserFromType(model any) (*Parser, error) {
	fields, err := getStructFields(model)
	if err != nil {
		return nil, err
	}
	return NewParser(fields), nil
}

func NewParser(defaultFields []FieldInfo) *Parser {
	return &Parser{
		DefaultFields:  defaultFields,
		MaxQueryLength: DefaultMaxQueryLength,
		MaxDepth:       DefaultMaxDepth,
		MaxTerms:       DefaultMaxTerms,
	}
}

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

		fields = append(fields, FieldInfo{
			Name:    jsonTag,
			IsJSONB: isJSONB,
		})
	}

	return fields, nil
}

func (p *Parser) ParseToMap(query string) (map[string]any, error) {
	// Security: Validate query length (OWASP A04: DoS prevention)
	if len(query) > p.MaxQueryLength {
		return nil, fmt.Errorf("query too long: %d bytes exceeds maximum of %d bytes", len(query), p.MaxQueryLength)
	}

	// Reset term counter for this parse
	p.termCount = 0

	node, err := p.parse(query)
	if err != nil {
		return nil, err
	}
	return p.enhancedNodeToMap(node), nil
}

func (p *Parser) ParseToSQL(query string) (string, []any, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to SQL: %s`, query))

	// Security: Validate query length (OWASP A04: DoS prevention)
	if len(query) > p.MaxQueryLength {
		return "", nil, fmt.Errorf("query too long: %d bytes exceeds maximum of %d bytes", len(query), p.MaxQueryLength)
	}

	// Reset term counter for this parse
	p.termCount = 0

	node, err := p.parse(query)
	if err != nil {
		return "", nil, err
	}

	return p.enhancedNodeToSQL(node)
}

func (p *Parser) ParseToDynamoDBPartiQL(query string) (string, []types.AttributeValue, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to DynamoDB PartiQL: %s`, query))

	// Security: Validate query length (OWASP A04: DoS prevention)
	if len(query) > p.MaxQueryLength {
		return "", nil, fmt.Errorf("query too long: %d bytes exceeds maximum of %d bytes", len(query), p.MaxQueryLength)
	}

	// Reset term counter for this parse
	p.termCount = 0

	node, err := p.parse(query)
	if err != nil {
		return "", nil, err
	}

	return p.enhancedNodeToDynamoDBPartiQL(node)
}

// parse parses the query string into an enhanced AST using the lexer
func (p *Parser) parse(query string) (*EnhancedNode, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	p.lexer = NewLexer(query)
	p.advance() // Load first token
	p.advance() // Load peek token

	return p.parseExpressionWithDepth(0)
}

// advance moves to the next token
func (p *Parser) advance() {
	p.current = p.peek
	p.peek = p.lexer.NextToken()
}

// parseExpressionWithDepth parses with depth tracking
func (p *Parser) parseExpressionWithDepth(depth int) (*EnhancedNode, error) {
	// Security: Check nesting depth (OWASP A04: DoS prevention)
	if depth > p.MaxDepth {
		return nil, fmt.Errorf("query nesting too deep: depth %d exceeds maximum of %d", depth, p.MaxDepth)
	}
	return p.parseOrWithDepth(depth)
}

// parseOrWithDepth handles OR operations with depth tracking
func (p *Parser) parseOrWithDepth(depth int) (*EnhancedNode, error) {
	left, err := p.parseAndWithDepth(depth + 1)
	if err != nil {
		return nil, err
	}

	for p.current.Type == TokenOR {
		p.advance()
		right, err := p.parseAndWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}

		// Convert enhanced nodes to proper nodes before combining
		leftNode := p.enhancedNodeToNode(left)
		rightNode := p.enhancedNodeToNode(right)

		left = &EnhancedNode{
			Node: &Node{
				Type:     NodeLogical,
				Operator: OR,
				Children: []*Node{leftNode, rightNode},
			},
		}
	}

	return left, nil
}

// parseAndWithDepth handles AND operations with depth tracking
func (p *Parser) parseAndWithDepth(depth int) (*EnhancedNode, error) {
	left, err := p.parseUnaryWithDepth(depth + 1)
	if err != nil {
		return nil, err
	}

	for p.current.Type == TokenAND || p.isImplicitAnd() {
		if p.current.Type == TokenAND {
			p.advance()
		}
		// Implicit AND: if we see another term without an operator
		right, err := p.parseUnaryWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}

		// Convert enhanced nodes to proper nodes before combining
		leftNode := p.enhancedNodeToNode(left)
		rightNode := p.enhancedNodeToNode(right)

		left = &EnhancedNode{
			Node: &Node{
				Type:     NodeLogical,
				Operator: AND,
				Children: []*Node{leftNode, rightNode},
			},
		}
	}

	return left, nil
}

// isImplicitAnd checks if we should treat the next token as an implicit AND
func (p *Parser) isImplicitAnd() bool {
	// If we see a new term start without an operator, it's implicit AND
	switch p.current.Type {
	case TokenIdent, TokenString, TokenPlus, TokenMinus, TokenNOT, TokenLParen:
		return true
	}
	return false
}

// parseUnaryWithDepth handles unary operations with depth tracking
func (p *Parser) parseUnaryWithDepth(depth int) (*EnhancedNode, error) {
	// Handle NOT
	if p.current.Type == TokenNOT {
		p.advance()
		expr, err := p.parsePrimaryWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}

		return &EnhancedNode{
			Node: &Node{
				Type:     NodeLogical,
				Operator: NOT,
				Children: []*Node{expr.Node},
			},
		}, nil
	}

	// Handle required (+)
	if p.current.Type == TokenPlus {
		p.advance()
		expr, err := p.parsePrimaryWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}
		expr.Required = true
		return expr, nil
	}

	// Handle prohibited (-)
	if p.current.Type == TokenMinus {
		p.advance()
		expr, err := p.parsePrimaryWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}
		expr.Prohibited = true
		return expr, nil
	}

	return p.parsePrimaryWithDepth(depth + 1)
}

// parsePrimaryWithDepth handles primary expressions with depth tracking
func (p *Parser) parsePrimaryWithDepth(depth int) (*EnhancedNode, error) {
	// Handle grouped expressions
	if p.current.Type == TokenLParen {
		p.advance()
		expr, err := p.parseExpressionWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}
		if p.current.Type != TokenRParen {
			return nil, fmt.Errorf("expected ')', got %v", p.current.Value)
		}
		p.advance()
		return expr, nil
	}

	// Handle quoted phrases
	if p.current.Type == TokenString {
		return p.parsePhrase()
	}

	// Handle field:value or implicit search
	return p.parseTerm()
}

// parsePhrase handles quoted phrase searches
func (p *Parser) parsePhrase() (*EnhancedNode, error) {
	// Security: Check term count (OWASP A04: DoS prevention)
	p.termCount++
	if p.termCount > p.MaxTerms {
		return nil, fmt.Errorf("too many terms: %d exceeds maximum of %d", p.termCount, p.MaxTerms)
	}

	phrase := p.current.Value
	p.advance()

	// Check for proximity (~n)
	proximity := 0
	if p.current.Type == TokenTilde {
		p.advance()
		if p.current.Type == TokenNumber {
			fmt.Sscanf(p.current.Value, "%d", &proximity)
			p.advance()
		}
	}

	// Check for boost (^n)
	boost := 0.0
	if p.current.Type == TokenCaret {
		p.advance()
		if p.current.Type == TokenNumber {
			fmt.Sscanf(p.current.Value, "%f", &boost)
			p.advance()
		}
	}

	// For now, treat phrase as a term with the full phrase value
	// In SQL, this will be handled as a LIKE or exact match
	node := &EnhancedNode{
		Node: &Node{
			Type:  NodeTerm,
			Value: phrase,
		},
		IsPhrase:  true,
		Proximity: proximity,
		Boost:     boost,
	}

	return node, nil
}

// parseTerm handles field:value terms and ranges
func (p *Parser) parseTerm() (*EnhancedNode, error) {
	// Check for range query [min TO max] or {min TO max}
	if p.current.Type == TokenLBracket || p.current.Type == TokenLBrace {
		return p.parseRange("")
	}

	// Get the field or value
	if p.current.Type != TokenIdent && p.current.Type != TokenNumber {
		return nil, fmt.Errorf("expected identifier or number, got %v", p.current.Value)
	}

	fieldOrValue := p.current.Value
	p.advance()

	// Check if this is a field:value pair
	if p.current.Type == TokenColon {
		p.advance()

		// Check for range after colon
		if p.current.Type == TokenLBracket || p.current.Type == TokenLBrace {
			return p.parseRange(fieldOrValue)
		}

		// Check for quoted phrase after colon
		if p.current.Type == TokenString {
			// Security: Check term count (OWASP A04: DoS prevention)
			p.termCount++
			if p.termCount > p.MaxTerms {
				return nil, fmt.Errorf("too many terms: %d exceeds maximum of %d", p.termCount, p.MaxTerms)
			}

			phrase := p.current.Value
			p.advance()

			// Check for proximity
			proximity := 0
			if p.current.Type == TokenTilde {
				p.advance()
				if p.current.Type == TokenNumber {
					fmt.Sscanf(p.current.Value, "%d", &proximity)
					p.advance()
				}
			}

			// Check for boost
			boost := 0.0
			if p.current.Type == TokenCaret {
				p.advance()
				if p.current.Type == TokenNumber {
					fmt.Sscanf(p.current.Value, "%f", &boost)
					p.advance()
				}
			}

			formattedField := p.formatFieldName(fieldOrValue)
			node := &EnhancedNode{
				Node: &Node{
					Type:  NodeTerm,
					Field: formattedField,
					Value: phrase,
				},
				IsPhrase:  true,
				Proximity: proximity,
				Boost:     boost,
			}
			return node, nil
		}

		// Regular field:value (or wildcard value)
		if p.current.Type != TokenIdent && p.current.Type != TokenNumber && p.current.Type != TokenWildcard {
			return nil, fmt.Errorf("expected value after ':', got %v", p.current.Value)
		}

		// Security: Check term count (OWASP A04: DoS prevention)
		p.termCount++
		if p.termCount > p.MaxTerms {
			return nil, fmt.Errorf("too many terms: %d exceeds maximum of %d", p.termCount, p.MaxTerms)
		}

		// Build the value, potentially including wildcards
		var value string
		if p.current.Type == TokenWildcard {
			value = p.current.Value
			p.advance()
			// Continue building the value if there's more
			for p.current.Type == TokenIdent || p.current.Type == TokenWildcard {
				value += p.current.Value
				p.advance()
			}
		} else {
			value = p.current.Value
			p.advance()
		}

		// Check for fuzzy (~n)
		fuzzy := 0
		if p.current.Type == TokenTilde {
			p.advance()
			if p.current.Type == TokenNumber {
				fmt.Sscanf(p.current.Value, "%d", &fuzzy)
				p.advance()
			} else {
				fuzzy = 2 // Default fuzzy distance
			}
		}

		// Check for boost (^n)
		boost := 0.0
		if p.current.Type == TokenCaret {
			p.advance()
			if p.current.Type == TokenNumber {
				fmt.Sscanf(p.current.Value, "%f", &boost)
				p.advance()
			}
		}

		formattedField := p.formatFieldName(fieldOrValue)

		// Determine if this is a wildcard query
		nodeType := NodeTerm
		matchType := matchExact
		processedValue := value

		if strings.Contains(value, "*") || strings.Contains(value, "?") {
			nodeType = NodeWildcard
			if strings.HasPrefix(value, "*") && strings.HasSuffix(value, "*") {
				matchType = matchContains
				processedValue = strings.Trim(value, "*")
			} else if strings.HasPrefix(value, "*") {
				matchType = matchEndsWith
				processedValue = strings.TrimPrefix(value, "*")
			} else if strings.HasSuffix(value, "*") {
				matchType = matchStartsWith
				processedValue = strings.TrimSuffix(value, "*")
			} else {
				matchType = matchContains
				processedValue = strings.ReplaceAll(strings.ReplaceAll(value, "*", "%"), "?", "_")
			}
		}

		node := &EnhancedNode{
			Node: &Node{
				Type:      nodeType,
				Field:     formattedField,
				Value:     processedValue,
				MatchType: matchType,
			},
			Fuzzy: fuzzy,
			Boost: boost,
		}

		return node, nil
	}

	// No colon, so this is an implicit search
	return p.createImplicitSearch(fieldOrValue)
}

// parseRange handles range queries [min TO max] or {min TO max}
func (p *Parser) parseRange(field string) (*EnhancedNode, error) {
	// Security: Check term count (OWASP A04: DoS prevention)
	// Range queries count as terms (they expand to comparison operations)
	p.termCount++
	if p.termCount > p.MaxTerms {
		return nil, fmt.Errorf("too many terms: %d exceeds maximum of %d", p.termCount, p.MaxTerms)
	}

	inclusive := p.current.Type == TokenLBracket
	p.advance()

	// Get min value
	if p.current.Type != TokenIdent && p.current.Type != TokenNumber && p.current.Value != "*" {
		return nil, fmt.Errorf("expected min value in range, got %v", p.current.Value)
	}
	min := p.current.Value
	p.advance()

	// Expect TO
	if p.current.Type != TokenTO {
		return nil, fmt.Errorf("expected TO in range query, got %v", p.current.Value)
	}
	p.advance()

	// Get max value
	if p.current.Type != TokenIdent && p.current.Type != TokenNumber && p.current.Value != "*" {
		return nil, fmt.Errorf("expected max value in range, got %v", p.current.Value)
	}
	max := p.current.Value
	p.advance()

	// Expect closing bracket/brace
	expectedClose := TokenRBracket
	if !inclusive {
		expectedClose = TokenRBrace
	}
	if p.current.Type != expectedClose {
		return nil, fmt.Errorf("expected closing bracket/brace in range")
	}
	p.advance()

	// Check for boost
	boost := 0.0
	if p.current.Type == TokenCaret {
		p.advance()
		if p.current.Type == TokenNumber {
			fmt.Sscanf(p.current.Value, "%f", &boost)
			p.advance()
		}
	}

	formattedField := field
	if field != "" {
		formattedField = p.formatFieldName(field)
	}

	node := &EnhancedNode{
		Node: &Node{
			Type:  NodeTerm, // Use NodeTerm as placeholder, actual handling via RangeInfo
			Field: formattedField,
		},
		RangeInfo: &RangeNode{
			Field:     formattedField,
			Min:       min,
			Max:       max,
			Inclusive: inclusive,
		},
		Boost: boost,
	}

	return node, nil
}

// createImplicitSearch creates an OR query across default fields
func (p *Parser) createImplicitSearch(term string) (*EnhancedNode, error) {
	slog.Debug(fmt.Sprintf(`Handling implicit search: %s`, term))

	if len(p.DefaultFields) == 0 {
		return nil, fmt.Errorf("no default fields for implicit search")
	}

	// Create OR node with children for each default field
	var children []*Node
	for _, field := range p.DefaultFields {
		// Security: Check term count (OWASP A04: DoS prevention)
		// Each default field counts as a term in implicit search
		p.termCount++
		if p.termCount > p.MaxTerms {
			return nil, fmt.Errorf("too many terms: %d exceeds maximum of %d", p.termCount, p.MaxTerms)
		}

		formattedField := p.formatFieldName(field.Name)

		// Determine if wildcard
		nodeType := NodeTerm
		matchType := matchContains // Default to contains for implicit search
		processedValue := term

		if strings.Contains(term, "*") || strings.Contains(term, "?") {
			nodeType = NodeWildcard
			if strings.HasPrefix(term, "*") && strings.HasSuffix(term, "*") {
				matchType = matchContains
				processedValue = strings.Trim(term, "*")
			} else if strings.HasPrefix(term, "*") {
				matchType = matchEndsWith
				processedValue = strings.TrimPrefix(term, "*")
			} else if strings.HasSuffix(term, "*") {
				matchType = matchStartsWith
				processedValue = strings.TrimSuffix(term, "*")
			} else {
				matchType = matchContains
				processedValue = strings.ReplaceAll(strings.ReplaceAll(term, "*", "%"), "?", "_")
			}
		} else {
			nodeType = NodeWildcard // Use wildcard with contains for implicit
		}

		children = append(children, &Node{
			Type:      nodeType,
			Field:     formattedField,
			Value:     processedValue,
			MatchType: matchType,
		})
	}

	node := &EnhancedNode{
		Node: &Node{
			Type:     NodeLogical,
			Operator: OR,
			Children: children,
		},
	}

	return node, nil
}

// enhancedNodeToNode converts an EnhancedNode to a plain Node, applying all enhancements
func (p *Parser) enhancedNodeToNode(enode *EnhancedNode) *Node {
	if enode == nil || enode.Node == nil {
		return nil
	}

	node := enode.Node

	// Handle range queries - convert to logical AND node with comparison nodes
	if enode.RangeInfo != nil {
		var children []*Node

		// Add min condition
		if enode.RangeInfo.Min != "*" {
			op := OpGreaterThanOrEqual
			if !enode.RangeInfo.Inclusive {
				op = OpGreaterThan
			}
			children = append(children, &Node{
				Type:       NodeTerm,
				Field:      enode.RangeInfo.Field,
				Value:      enode.RangeInfo.Min,
				Comparison: op,
			})
		}

		// Add max condition
		if enode.RangeInfo.Max != "*" {
			op := OpLessThanOrEqual
			if !enode.RangeInfo.Inclusive {
				op = OpLessThan
			}
			children = append(children, &Node{
				Type:       NodeTerm,
				Field:      enode.RangeInfo.Field,
				Value:      enode.RangeInfo.Max,
				Comparison: op,
			})
		}

		if len(children) == 0 {
			// No conditions, return empty node
			return nil
		} else if len(children) == 1 {
			node = children[0]
		} else {
			node = &Node{
				Type:     NodeLogical,
				Operator: AND,
				Children: children,
			}
		}
	}

	// Handle prohibited - wrap in NOT
	if enode.Prohibited {
		node = &Node{
			Type:     NodeLogical,
			Operator: NOT,
			Children: []*Node{node},
		}
	}

	// Fuzzy is already handled in the node conversion
	// Boost is ignored (no SQL support)
	// Required doesn't need special handling (implicit in AND)

	return node
}

func (p *Parser) formatFieldName(fieldName string) string {
	if parts := strings.SplitN(fieldName, ".", 2); len(parts) == 2 {
		baseField := parts[0]
		subField := parts[1]

		for _, field := range p.DefaultFields {
			if field.IsJSONB && field.Name == baseField {
				return fmt.Sprintf("%s->>'%s'", baseField, subField)
			}
		}
	}
	return fieldName
}

// Helper conversion methods

func (p *Parser) enhancedNodeToMap(node *EnhancedNode) map[string]any {
	if node == nil || node.Node == nil {
		return nil
	}

	var result map[string]any
	
	// Convert base node to map
	switch node.Node.Type {
	case NodeTerm:
		result = map[string]any{node.Node.Field: node.Node.Value}
	case NodeWildcard:
		result = map[string]any{node.Node.Field: map[string]string{
			"$like": wildcardToPattern(node.Node.Value, node.Node.MatchType),
		}}
	case NodeLogical:
		result = make(map[string]any)
		children := make([]map[string]any, 0, len(node.Node.Children))
		for _, child := range node.Node.Children {
			// Convert plain node to enhanced node for recursive processing
			enhancedChild := &EnhancedNode{Node: child}
			children = append(children, p.enhancedNodeToMap(enhancedChild))
		}
		result[string(node.Node.Operator)] = children
	default:
		result = make(map[string]any)
	}

	// Add enhanced fields
	if node.Required {
		result["$required"] = true
	}
	if node.Prohibited {
		result["$prohibited"] = true
	}
	if node.Boost > 0 {
		result["$boost"] = node.Boost
	}
	if node.Proximity > 0 {
		result["$proximity"] = node.Proximity
	}
	if node.Fuzzy > 0 {
		result["$fuzzy"] = node.Fuzzy
	}
	if node.RangeInfo != nil {
		result["$range"] = map[string]any{
			"min":       node.RangeInfo.Min,
			"max":       node.RangeInfo.Max,
			"inclusive": node.RangeInfo.Inclusive,
		}
	}

	return result
}

func (p *Parser) enhancedNodeToSQL(node *EnhancedNode) (string, []any, error) {
	if node == nil || node.Node == nil {
		return "", nil, nil
	}

	// Handle range queries
	if node.RangeInfo != nil {
		return p.rangeToSQL(node.RangeInfo)
	}

	// Handle prohibited (NOT)
	if node.Prohibited {
		sql, params, err := p.enhancedNodeToSQLInternal(node.Node)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("NOT (%s)", sql), params, nil
	}

	// For required, just process normally (required is implicit in AND)
	// For boost, ignore in SQL (no relevance scoring)
	// For fuzzy, approximate with LIKE wildcards
	if node.Fuzzy > 0 && node.Node.Type == NodeTerm {
		// Convert fuzzy to wildcard search
		node.Node.Type = NodeWildcard
		node.Node.MatchType = matchContains
	}

	// For proximity searches, treat as phrase match (best effort)
	// SQL doesn't have native proximity, so we'll just do exact match

	return p.enhancedNodeToSQLInternal(node.Node)
}

// enhancedNodeToSQLInternal converts a node to SQL (internal helper)
func (p *Parser) enhancedNodeToSQLInternal(node *Node) (string, []any, error) {
	if node == nil {
		return "", nil, nil
	}

	switch node.Type {
	case NodeTerm:
		// Use comparison operator if specified, otherwise default to =
		op := string(node.Comparison)
		if op == "" {
			op = "="
		}

		if strings.Contains(node.Field, "->>") {
			return fmt.Sprintf("%s %s ?", node.Field, op), []any{node.Value}, nil
		}
		return fmt.Sprintf("%s %s ?", node.Field, op), []any{node.Value}, nil
	case NodeWildcard:
		pattern := wildcardToPattern(node.Value, node.MatchType)
		if strings.Contains(node.Field, "->>") {
			return fmt.Sprintf("%s ILIKE ?", node.Field), []any{pattern}, nil
		} else {
			return fmt.Sprintf("%s::text ILIKE ?", node.Field), []any{pattern}, nil
		}
	case NodeLogical:
		var parts []string
		var params []any

		for _, child := range node.Children {
			sqlPart, childParams, err := p.enhancedNodeToSQLInternal(child)
			if err != nil {
				return "", nil, err
			}
			if sqlPart != "" {
				parts = append(parts, sqlPart)
				params = append(params, childParams...)
			}
		}

		if len(parts) == 0 {
			return "", nil, nil
		}

		// Special handling for NOT operator - always wrap in NOT(...) even with single child
		if node.Operator == NOT {
			if len(parts) == 1 {
				return fmt.Sprintf("NOT (%s)", parts[0]), params, nil
			}
			// NOT with multiple children - wrap each in NOT and AND them
			notParts := make([]string, len(parts))
			for i, part := range parts {
				notParts[i] = fmt.Sprintf("NOT (%s)", part)
			}
			return fmt.Sprintf("(%s)", strings.Join(notParts, " AND ")), params, nil
		}

		if len(parts) == 1 {
			return parts[0], params, nil
		}

		operator := string(node.Operator)
		if node.Negate {
			operator = "NOT " + operator
		}

		return fmt.Sprintf("(%s)", strings.Join(parts, fmt.Sprintf(" %s ", operator))), params, nil
	}

	return "", nil, fmt.Errorf("unsupported node type: %v", node.Type)
}

func (p *Parser) rangeToSQL(rangeInfo *RangeNode) (string, []any, error) {
	if rangeInfo == nil {
		return "", nil, nil
	}

	var conditions []string
	var params []any

	// Handle min value
	if rangeInfo.Min != "*" {
		if rangeInfo.Inclusive {
			conditions = append(conditions, fmt.Sprintf("%s >= ?", rangeInfo.Field))
		} else {
			conditions = append(conditions, fmt.Sprintf("%s > ?", rangeInfo.Field))
		}
		params = append(params, rangeInfo.Min)
	}

	// Handle max value
	if rangeInfo.Max != "*" {
		if rangeInfo.Inclusive {
			conditions = append(conditions, fmt.Sprintf("%s <= ?", rangeInfo.Field))
		} else {
			conditions = append(conditions, fmt.Sprintf("%s < ?", rangeInfo.Field))
		}
		params = append(params, rangeInfo.Max)
	}

	if len(conditions) == 0 {
		return "", nil, nil
	}

	return strings.Join(conditions, " AND "), params, nil
}

func (p *Parser) enhancedNodeToDynamoDBPartiQL(node *EnhancedNode) (string, []types.AttributeValue, error) {
	if node == nil || node.Node == nil {
		return "", nil, nil
	}

	// Handle range queries
	if node.RangeInfo != nil {
		return p.rangeToDynamoDBPartiQL(node.RangeInfo)
	}

	// Handle prohibited (NOT)
	if node.Prohibited {
		sql, params, err := p.enhancedNodeToDynamoDBPartiQLInternal(node.Node)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("NOT (%s)", sql), params, nil
	}

	// For fuzzy, approximate with contains
	if node.Fuzzy > 0 && node.Node.Type == NodeTerm {
		node.Node.Type = NodeWildcard
		node.Node.MatchType = matchContains
	}

	return p.enhancedNodeToDynamoDBPartiQLInternal(node.Node)
}

// enhancedNodeToDynamoDBPartiQLInternal converts a node to DynamoDB PartiQL (internal helper)
func (p *Parser) enhancedNodeToDynamoDBPartiQLInternal(node *Node) (string, []types.AttributeValue, error) {
	if node == nil {
		return "", nil, nil
	}

	switch node.Type {
	case NodeTerm:
		// Use comparison operator if specified, otherwise default to =
		op := string(node.Comparison)
		if op == "" {
			op = "="
		}
		return fmt.Sprintf("%s %s ?", node.Field, op), []types.AttributeValue{
			&types.AttributeValueMemberS{Value: node.Value},
		}, nil
	case NodeWildcard:
		// For wildcard node, use begins_with or contains based on the match type
		switch node.MatchType {
		case matchStartsWith:
			return fmt.Sprintf("begins_with(%s, ?)", node.Field), []types.AttributeValue{
				&types.AttributeValueMemberS{Value: node.Value},
			}, nil
		case matchEndsWith, matchContains:
			return fmt.Sprintf("contains(%s, ?)", node.Field), []types.AttributeValue{
				&types.AttributeValueMemberS{Value: node.Value},
			}, nil
		default:
			return fmt.Sprintf("%s = ?", node.Field), []types.AttributeValue{
				&types.AttributeValueMemberS{Value: node.Value},
			}, nil
		}
	case NodeLogical:
		// For logical node, combine conditions with appropriate operator
		var parts []string
		var params []types.AttributeValue

		for _, child := range node.Children {
			part, childParams, err := p.enhancedNodeToDynamoDBPartiQLInternal(child)
			if err != nil {
				return "", nil, err
			}
			if part != "" {
				parts = append(parts, part)
				params = append(params, childParams...)
			}
		}

		if len(parts) == 0 {
			return "", nil, nil
		}

		// Special handling for NOT operator - always wrap in NOT(...) even with single child
		if node.Operator == NOT {
			if len(parts) == 1 {
				return fmt.Sprintf("NOT (%s)", parts[0]), params, nil
			}
			// NOT with multiple children - wrap each in NOT and AND them
			notParts := make([]string, len(parts))
			for i, part := range parts {
				notParts[i] = fmt.Sprintf("NOT (%s)", part)
			}
			return fmt.Sprintf("(%s)", strings.Join(notParts, " AND ")), params, nil
		}

		if len(parts) == 1 {
			return parts[0], params, nil
		}

		operator := string(node.Operator)
		if node.Negate {
			operator = "NOT " + operator
		}

		return fmt.Sprintf("(%s)", strings.Join(parts, fmt.Sprintf(" %s ", operator))), params, nil
	}

	return "", nil, fmt.Errorf("unsupported node type: %v", node.Type)
}

func (p *Parser) rangeToDynamoDBPartiQL(rangeInfo *RangeNode) (string, []types.AttributeValue, error) {
	if rangeInfo == nil {
		return "", nil, nil
	}

	var conditions []string
	var params []types.AttributeValue

	// Handle min value
	if rangeInfo.Min != "*" {
		if rangeInfo.Inclusive {
			conditions = append(conditions, fmt.Sprintf("%s >= ?", rangeInfo.Field))
		} else {
			conditions = append(conditions, fmt.Sprintf("%s > ?", rangeInfo.Field))
		}
		params = append(params, &types.AttributeValueMemberS{Value: rangeInfo.Min})
	}

	// Handle max value
	if rangeInfo.Max != "*" {
		if rangeInfo.Inclusive {
			conditions = append(conditions, fmt.Sprintf("%s <= ?", rangeInfo.Field))
		} else {
			conditions = append(conditions, fmt.Sprintf("%s < ?", rangeInfo.Field))
		}
		params = append(params, &types.AttributeValueMemberS{Value: rangeInfo.Max})
	}

	if len(conditions) == 0 {
		return "", nil, nil
	}

	return strings.Join(conditions, " AND "), params, nil
}

func wildcardToPattern(value string, matchType MatchType) string {
	switch matchType {
	case matchStartsWith:
		return value + "%"
	case matchEndsWith:
		return "%" + value
	case matchContains:
		return "%" + value + "%"
	default:
		return value
	}
}
