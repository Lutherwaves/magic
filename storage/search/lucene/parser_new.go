package lucene

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Enhanced node types for full Lucene support
const (
	NodePhrase NodeType = iota + 100 // Start from 100 to avoid conflicts
	NodeRange
	NodeFuzzy
	NodeProximity
)

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
	Required  bool    // + operator
	Prohibited bool   // - operator
	Boost     float64 // ^ operator
	Proximity int     // ~n for phrases
	Fuzzy     int     // ~n for terms
	IsPhrase  bool    // quoted string
	RangeInfo *RangeNode
}

// EnhancedParser is a new parser using the lexer for full Lucene syntax
type EnhancedParser struct {
	*Parser
	lexer   *Lexer
	current Token
	peek    Token
}

// NewEnhancedParser creates a new enhanced parser with lexer support
func NewEnhancedParser(defaultFields []FieldInfo) *EnhancedParser {
	return &EnhancedParser{
		Parser: NewParser(defaultFields),
	}
}

// NewEnhancedParserFromType creates parser from struct type
func NewEnhancedParserFromType(model any) (*EnhancedParser, error) {
	fields, err := getStructFields(model)
	if err != nil {
		return nil, err
	}
	return NewEnhancedParser(fields), nil
}

// ParseToSQL parses query and returns SQL WHERE clause
func (ep *EnhancedParser) ParseToSQL(query string) (string, []any, error) {
	slog.Debug(fmt.Sprintf(`Parsing enhanced query to SQL: %s`, query))

	node, err := ep.Parse(query)
	if err != nil {
		return "", nil, err
	}

	return ep.enhancedNodeToSQL(node)
}

// ParseToMap parses query and returns map representation
func (ep *EnhancedParser) ParseToMap(query string) (map[string]any, error) {
	node, err := ep.Parse(query)
	if err != nil {
		return nil, err
	}
	return ep.enhancedNodeToMap(node), nil
}

// ParseToDynamoDBPartiQL parses query and returns DynamoDB PartiQL
func (ep *EnhancedParser) ParseToDynamoDBPartiQL(query string) (string, []types.AttributeValue, error) {
	slog.Debug(fmt.Sprintf(`Parsing enhanced query to DynamoDB PartiQL: %s`, query))

	node, err := ep.Parse(query)
	if err != nil {
		return "", nil, err
	}

	return ep.enhancedNodeToDynamoDBPartiQL(node)
}

// Parse parses the query string into an enhanced AST
func (ep *EnhancedParser) Parse(query string) (*EnhancedNode, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	ep.lexer = NewLexer(query)
	ep.advance() // Load first token
	ep.advance() // Load peek token

	return ep.parseExpression()
}

// advance moves to the next token
func (ep *EnhancedParser) advance() {
	ep.current = ep.peek
	ep.peek = ep.lexer.NextToken()
}

// parseExpression parses the top-level expression (handles OR)
func (ep *EnhancedParser) parseExpression() (*EnhancedNode, error) {
	return ep.parseOr()
}

// parseOr handles OR operations
func (ep *EnhancedParser) parseOr() (*EnhancedNode, error) {
	left, err := ep.parseAnd()
	if err != nil {
		return nil, err
	}

	for ep.current.Type == TokenOR {
		ep.advance()
		right, err := ep.parseAnd()
		if err != nil {
			return nil, err
		}

		// Convert enhanced nodes to proper nodes before combining
		leftNode := ep.enhancedNodeToNode(left)
		rightNode := ep.enhancedNodeToNode(right)

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

// enhancedNodeToNode converts an EnhancedNode to a plain Node, applying all enhancements
func (ep *EnhancedParser) enhancedNodeToNode(enode *EnhancedNode) *Node {
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

// parseAnd handles AND operations and implicit AND
func (ep *EnhancedParser) parseAnd() (*EnhancedNode, error) {
	left, err := ep.parseUnary()
	if err != nil {
		return nil, err
	}

	for ep.current.Type == TokenAND || ep.isImplicitAnd() {
		if ep.current.Type == TokenAND {
			ep.advance()
		}
		// Implicit AND: if we see another term without an operator
		right, err := ep.parseUnary()
		if err != nil {
			return nil, err
		}

		// Convert enhanced nodes to proper nodes before combining
		leftNode := ep.enhancedNodeToNode(left)
		rightNode := ep.enhancedNodeToNode(right)

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
func (ep *EnhancedParser) isImplicitAnd() bool {
	// If we see a new term start without an operator, it's implicit AND
	switch ep.current.Type {
	case TokenIdent, TokenString, TokenPlus, TokenMinus, TokenNOT, TokenLParen:
		return true
	}
	return false
}

// parseUnary handles NOT, required (+), and prohibited (-) operations
func (ep *EnhancedParser) parseUnary() (*EnhancedNode, error) {
	// Handle NOT
	if ep.current.Type == TokenNOT {
		ep.advance()
		expr, err := ep.parsePrimary()
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
	if ep.current.Type == TokenPlus {
		ep.advance()
		expr, err := ep.parsePrimary()
		if err != nil {
			return nil, err
		}
		expr.Required = true
		return expr, nil
	}

	// Handle prohibited (-)
	if ep.current.Type == TokenMinus {
		ep.advance()
		expr, err := ep.parsePrimary()
		if err != nil {
			return nil, err
		}
		expr.Prohibited = true
		return expr, nil
	}

	return ep.parsePrimary()
}

// parsePrimary handles primary expressions (terms, phrases, groups, ranges)
func (ep *EnhancedParser) parsePrimary() (*EnhancedNode, error) {
	// Handle grouped expressions
	if ep.current.Type == TokenLParen {
		ep.advance()
		expr, err := ep.parseExpression()
		if err != nil {
			return nil, err
		}
		if ep.current.Type != TokenRParen {
			return nil, fmt.Errorf("expected ')', got %v", ep.current.Value)
		}
		ep.advance()
		return expr, nil
	}

	// Handle quoted phrases
	if ep.current.Type == TokenString {
		return ep.parsePhrase()
	}

	// Handle field:value or implicit search
	return ep.parseTerm()
}

// parsePhrase handles quoted phrase searches
func (ep *EnhancedParser) parsePhrase() (*EnhancedNode, error) {
	phrase := ep.current.Value
	ep.advance()

	// Check for proximity (~n)
	proximity := 0
	if ep.current.Type == TokenTilde {
		ep.advance()
		if ep.current.Type == TokenNumber {
			fmt.Sscanf(ep.current.Value, "%d", &proximity)
			ep.advance()
		}
	}

	// Check for boost (^n)
	boost := 0.0
	if ep.current.Type == TokenCaret {
		ep.advance()
		if ep.current.Type == TokenNumber {
			fmt.Sscanf(ep.current.Value, "%f", &boost)
			ep.advance()
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
func (ep *EnhancedParser) parseTerm() (*EnhancedNode, error) {
	// Check for range query [min TO max] or {min TO max}
	if ep.current.Type == TokenLBracket || ep.current.Type == TokenLBrace {
		return ep.parseRange("")
	}

	// Get the field or value
	if ep.current.Type != TokenIdent && ep.current.Type != TokenNumber {
		return nil, fmt.Errorf("expected identifier or number, got %v", ep.current.Value)
	}

	fieldOrValue := ep.current.Value
	ep.advance()

	// Check if this is a field:value pair
	if ep.current.Type == TokenColon {
		ep.advance()

		// Check for range after colon
		if ep.current.Type == TokenLBracket || ep.current.Type == TokenLBrace {
			return ep.parseRange(fieldOrValue)
		}

		// Check for quoted phrase after colon
		if ep.current.Type == TokenString {
			phrase := ep.current.Value
			ep.advance()

			// Check for proximity
			proximity := 0
			if ep.current.Type == TokenTilde {
				ep.advance()
				if ep.current.Type == TokenNumber {
					fmt.Sscanf(ep.current.Value, "%d", &proximity)
					ep.advance()
				}
			}

			// Check for boost
			boost := 0.0
			if ep.current.Type == TokenCaret {
				ep.advance()
				if ep.current.Type == TokenNumber {
					fmt.Sscanf(ep.current.Value, "%f", &boost)
					ep.advance()
				}
			}

			formattedField := ep.formatFieldName(fieldOrValue)
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
		if ep.current.Type != TokenIdent && ep.current.Type != TokenNumber && ep.current.Type != TokenWildcard {
			return nil, fmt.Errorf("expected value after ':', got %v", ep.current.Value)
		}

		// Build the value, potentially including wildcards
		var value string
		if ep.current.Type == TokenWildcard {
			value = ep.current.Value
			ep.advance()
			// Continue building the value if there's more
			for ep.current.Type == TokenIdent || ep.current.Type == TokenWildcard {
				value += ep.current.Value
				ep.advance()
			}
		} else {
			value = ep.current.Value
			ep.advance()
		}

		// Check for fuzzy (~n)
		fuzzy := 0
		if ep.current.Type == TokenTilde {
			ep.advance()
			if ep.current.Type == TokenNumber {
				fmt.Sscanf(ep.current.Value, "%d", &fuzzy)
				ep.advance()
			} else {
				fuzzy = 2 // Default fuzzy distance
			}
		}

		// Check for boost (^n)
		boost := 0.0
		if ep.current.Type == TokenCaret {
			ep.advance()
			if ep.current.Type == TokenNumber {
				fmt.Sscanf(ep.current.Value, "%f", &boost)
				ep.advance()
			}
		}

		formattedField := ep.formatFieldName(fieldOrValue)

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
	return ep.createImplicitSearch(fieldOrValue)
}

// parseRange handles range queries [min TO max] or {min TO max}
func (ep *EnhancedParser) parseRange(field string) (*EnhancedNode, error) {
	inclusive := ep.current.Type == TokenLBracket
	ep.advance()

	// Get min value
	if ep.current.Type != TokenIdent && ep.current.Type != TokenNumber && ep.current.Value != "*" {
		return nil, fmt.Errorf("expected min value in range, got %v", ep.current.Value)
	}
	min := ep.current.Value
	ep.advance()

	// Expect TO
	if ep.current.Type != TokenTO {
		return nil, fmt.Errorf("expected TO in range query, got %v", ep.current.Value)
	}
	ep.advance()

	// Get max value
	if ep.current.Type != TokenIdent && ep.current.Type != TokenNumber && ep.current.Value != "*" {
		return nil, fmt.Errorf("expected max value in range, got %v", ep.current.Value)
	}
	max := ep.current.Value
	ep.advance()

	// Expect closing bracket/brace
	expectedClose := TokenRBracket
	if !inclusive {
		expectedClose = TokenRBrace
	}
	if ep.current.Type != expectedClose {
		return nil, fmt.Errorf("expected closing bracket/brace in range")
	}
	ep.advance()

	// Check for boost
	boost := 0.0
	if ep.current.Type == TokenCaret {
		ep.advance()
		if ep.current.Type == TokenNumber {
			fmt.Sscanf(ep.current.Value, "%f", &boost)
			ep.advance()
		}
	}

	formattedField := field
	if field != "" {
		formattedField = ep.formatFieldName(field)
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
func (ep *EnhancedParser) createImplicitSearch(term string) (*EnhancedNode, error) {
	slog.Debug(fmt.Sprintf(`Handling implicit search: %s`, term))

	if len(ep.DefaultFields) == 0 {
		return nil, fmt.Errorf("no default fields for implicit search")
	}

	// Create OR node with children for each default field
	var children []*Node
	for _, field := range ep.DefaultFields {
		formattedField := ep.formatFieldName(field.Name)

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

// Helper conversion methods

func (ep *EnhancedParser) enhancedNodeToMap(node *EnhancedNode) map[string]any {
	if node == nil || node.Node == nil {
		return nil
	}

	result := ep.nodeToMap(node.Node)

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

func (ep *EnhancedParser) enhancedNodeToSQL(node *EnhancedNode) (string, []any, error) {
	if node == nil || node.Node == nil {
		return "", nil, nil
	}

	// Handle range queries
	if node.RangeInfo != nil {
		return ep.rangeToSQL(node.RangeInfo)
	}

	// Handle prohibited (NOT)
	if node.Prohibited {
		sql, params, err := ep.enhancedNodeToSQLInternal(node.Node)
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

	return ep.enhancedNodeToSQLInternal(node.Node)
}

// enhancedNodeToSQLInternal converts a node to SQL (internal helper)
func (ep *EnhancedParser) enhancedNodeToSQLInternal(node *Node) (string, []any, error) {
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
			sqlPart, childParams, err := ep.enhancedNodeToSQLInternal(child)
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

func (ep *EnhancedParser) rangeToSQL(rangeInfo *RangeNode) (string, []any, error) {
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

func (ep *EnhancedParser) enhancedNodeToDynamoDBPartiQL(node *EnhancedNode) (string, []types.AttributeValue, error) {
	if node == nil || node.Node == nil {
		return "", nil, nil
	}

	// Handle range queries
	if node.RangeInfo != nil {
		return ep.rangeToDynamoDBPartiQL(node.RangeInfo)
	}

	// Handle prohibited (NOT)
	if node.Prohibited {
		sql, params, err := ep.enhancedNodeToDynamoDBPartiQLInternal(node.Node)
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

	return ep.enhancedNodeToDynamoDBPartiQLInternal(node.Node)
}

// enhancedNodeToDynamoDBPartiQLInternal converts a node to DynamoDB PartiQL (internal helper)
func (ep *EnhancedParser) enhancedNodeToDynamoDBPartiQLInternal(node *Node) (string, []types.AttributeValue, error) {
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
			part, childParams, err := ep.enhancedNodeToDynamoDBPartiQLInternal(child)
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

func (ep *EnhancedParser) rangeToDynamoDBPartiQL(rangeInfo *RangeNode) (string, []types.AttributeValue, error) {
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
