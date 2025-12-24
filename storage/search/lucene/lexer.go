package lucene

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenType represents the type of token
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenError
	TokenIdent       // field names, values
	TokenString      // "quoted string"
	TokenNumber      // 123, 45.67
	TokenAND         // AND, &&
	TokenOR          // OR, ||
	TokenNOT         // NOT, !
	TokenPlus        // +
	TokenMinus       // -
	TokenColon       // :
	TokenLParen      // (
	TokenRParen      // )
	TokenLBracket    // [
	TokenRBracket    // ]
	TokenLBrace      // {
	TokenRBrace      // }
	TokenTO          // TO (for ranges)
	TokenTilde       // ~ (for fuzzy/proximity)
	TokenCaret       // ^ (for boosting)
	TokenWildcard    // *, ?
)

// Token represents a lexical token
type Token struct {
	Type  TokenType
	Value string
	Pos   int
}

// Lexer tokenizes Lucene query syntax
type Lexer struct {
	input string
	pos   int
	start int
	width int
}

// NewLexer creates a new lexer for the input string
func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
		pos:   0,
		start: 0,
		width: 0,
	}
}

// NextToken returns the next token from the input
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Pos: l.pos}
	}

	l.start = l.pos
	ch := l.peek()

	switch ch {
	case '"':
		return l.scanString()
	case '+':
		l.next()
		return Token{Type: TokenPlus, Value: "+", Pos: l.start}
	case '-':
		l.next()
		return Token{Type: TokenMinus, Value: "-", Pos: l.start}
	case ':':
		l.next()
		return Token{Type: TokenColon, Value: ":", Pos: l.start}
	case '(':
		l.next()
		return Token{Type: TokenLParen, Value: "(", Pos: l.start}
	case ')':
		l.next()
		return Token{Type: TokenRParen, Value: ")", Pos: l.start}
	case '[':
		l.next()
		return Token{Type: TokenLBracket, Value: "[", Pos: l.start}
	case ']':
		l.next()
		return Token{Type: TokenRBracket, Value: "]", Pos: l.start}
	case '{':
		l.next()
		return Token{Type: TokenLBrace, Value: "{", Pos: l.start}
	case '}':
		l.next()
		return Token{Type: TokenRBrace, Value: "}", Pos: l.start}
	case '~':
		l.next()
		return Token{Type: TokenTilde, Value: "~", Pos: l.start}
	case '^':
		l.next()
		return Token{Type: TokenCaret, Value: "^", Pos: l.start}
	case '*', '?':
		l.next()
		return Token{Type: TokenWildcard, Value: string(ch), Pos: l.start}
	case '&':
		if l.peekAhead(1) == '&' {
			l.next()
			l.next()
			return Token{Type: TokenAND, Value: "&&", Pos: l.start}
		}
		l.next()
		return Token{Type: TokenIdent, Value: "&", Pos: l.start}
	case '|':
		if l.peekAhead(1) == '|' {
			l.next()
			l.next()
			return Token{Type: TokenOR, Value: "||", Pos: l.start}
		}
		l.next()
		return Token{Type: TokenIdent, Value: "|", Pos: l.start}
	case '!':
		l.next()
		return Token{Type: TokenNOT, Value: "!", Pos: l.start}
	case '\\':
		// Handle escaped characters
		l.next()
		if l.pos < len(l.input) {
			escapedChar := l.next()
			return Token{Type: TokenIdent, Value: string(escapedChar), Pos: l.start}
		}
		return Token{Type: TokenError, Value: "unexpected end after backslash", Pos: l.start}
	default:
		if unicode.IsDigit(ch) || (ch == '-' && l.pos+1 < len(l.input) && unicode.IsDigit(rune(l.input[l.pos+1]))) {
			return l.scanNumber()
		}
		return l.scanIdent()
	}
}

// peek returns the current character without advancing
func (l *Lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return rune(l.input[l.pos])
}

// peekAhead returns the character at offset positions ahead
func (l *Lexer) peekAhead(offset int) rune {
	pos := l.pos + offset
	if pos >= len(l.input) {
		return 0
	}
	return rune(l.input[pos])
}

// next advances the position and returns the current character
func (l *Lexer) next() rune {
	if l.pos >= len(l.input) {
		l.width = 0
		return 0
	}
	ch := rune(l.input[l.pos])
	l.pos++
	l.width = 1
	return ch
}

// skipWhitespace skips all whitespace characters
func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.pos++
	}
}

// scanString scans a quoted string
func (l *Lexer) scanString() Token {
	l.next() // consume opening quote
	var value strings.Builder

	for {
		ch := l.peek()
		if ch == 0 {
			return Token{Type: TokenError, Value: "unterminated string", Pos: l.start}
		}
		if ch == '"' {
			l.next() // consume closing quote
			break
		}
		if ch == '\\' {
			l.next()
			if l.pos >= len(l.input) {
				return Token{Type: TokenError, Value: "unexpected end in string", Pos: l.start}
			}
			// Handle escaped character
			escapedChar := l.next()
			value.WriteRune(escapedChar)
		} else {
			value.WriteRune(l.next())
		}
	}

	return Token{Type: TokenString, Value: value.String(), Pos: l.start}
}

// scanNumber scans a number (integer or float) or date-like patterns
func (l *Lexer) scanNumber() Token {
	var value strings.Builder

	// Handle optional minus sign
	if l.peek() == '-' {
		value.WriteRune(l.next())
	}

	// Scan digits
	for l.pos < len(l.input) && unicode.IsDigit(l.peek()) {
		value.WriteRune(l.next())
	}

	// Handle decimal point or date separator
	if (l.peek() == '.' || l.peek() == '-') && l.pos+1 < len(l.input) && unicode.IsDigit(l.peekAhead(1)) {
		separator := l.peek()
		value.WriteRune(l.next()) // consume separator
		for l.pos < len(l.input) {
			ch := l.peek()
			if !unicode.IsDigit(ch) && ch != separator {
				break
			}
			value.WriteRune(l.next())
		}
	}

	return Token{Type: TokenNumber, Value: value.String(), Pos: l.start}
}

// scanIdent scans an identifier or keyword
func (l *Lexer) scanIdent() Token {
	var value strings.Builder

	for {
		ch := l.peek()
		if ch == 0 {
			break
		}

		// Check if we've hit a special character or whitespace
		if unicode.IsSpace(ch) || isSpecialChar(ch) {
			break
		}

		// Handle escaped characters
		if ch == '\\' {
			l.next()
			if l.pos < len(l.input) {
				value.WriteRune(l.next())
			}
			continue
		}

		// Handle wildcards as part of the identifier
		if ch == '*' || ch == '?' {
			value.WriteRune(l.next())
			continue
		}

		value.WriteRune(l.next())
	}

	str := value.String()

	// Check for keywords
	switch strings.ToUpper(str) {
	case "AND":
		return Token{Type: TokenAND, Value: str, Pos: l.start}
	case "OR":
		return Token{Type: TokenOR, Value: str, Pos: l.start}
	case "NOT":
		return Token{Type: TokenNOT, Value: str, Pos: l.start}
	case "TO":
		return Token{Type: TokenTO, Value: str, Pos: l.start}
	}

	return Token{Type: TokenIdent, Value: str, Pos: l.start}
}

// isSpecialChar checks if a character is a special Lucene operator
func isSpecialChar(ch rune) bool {
	return ch == ':' || ch == '(' || ch == ')' || ch == '[' || ch == ']' ||
		ch == '{' || ch == '}' || ch == '+' || ch == '-' || ch == '!' ||
		ch == '~' || ch == '^' || ch == '"' || ch == '&' || ch == '|'
}

// AllTokens returns all tokens from the input (useful for debugging)
func (l *Lexer) AllTokens() ([]Token, error) {
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF || tok.Type == TokenError {
			if tok.Type == TokenError {
				return tokens, fmt.Errorf("lexer error at position %d: %s", tok.Pos, tok.Value)
			}
			break
		}
	}
	return tokens, nil
}
