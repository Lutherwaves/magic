# Enhanced Lucene Query Parser

This package provides a full-featured Apache Lucene query parser implementation for Go, compatible with the [Apache Lucene Query Parser Syntax](https://lucene.apache.org/core/2_9_4/queryparsersyntax.html).

## Features

The parser supports the following Lucene query syntax features:

### ✅ Implemented Features

#### 1. **Basic Field Queries**
```
name:john
email:user@example.com
```

#### 2. **Wildcard Searches**
- Prefix: `name:john*`
- Suffix: `name:*smith`
- Contains: `name:*oh*`
- Single character: `name:j?hn`

#### 3. **Boolean Operators**
- AND: `name:john AND status:active`
- OR: `name:john OR name:jane`
- NOT: `name:john NOT status:inactive`

#### 4. **Alternative Boolean Operators**
- `&&` (AND): `name:john && status:active`
- `||` (OR): `name:john || name:jane`
- `!` (NOT): `name:john && !status:inactive`

#### 5. **Quoted Phrases**
```
description:"hello world"
title:"Apache Lucene"
```

#### 6. **Range Queries**
- Inclusive range (square brackets): `age:[18 TO 65]`
- Exclusive range (curly braces): `age:{18 TO 65}`
- Open-ended ranges: `age:[18 TO *]` or `created_at:[* TO 2024-12-31]`
- Date ranges: `created_at:[2024-01-01 TO 2024-12-31]`

#### 7. **Grouping**
```
(name:john OR name:jane) AND status:active
```

#### 8. **JSONB Field Access** (PostgreSQL)
```
metadata.key:value
settings.theme:dark
```

#### 9. **Implicit Search**
When no field is specified, searches across all default fields:
```
john              # Searches across all configured default fields
john*             # Wildcard search across default fields
```

#### 10. **Required Operator (+)**
```
+name:john        # name:john is required
```

#### 11. **Prohibited Operator (-)**
```
-status:inactive  # Excludes status:inactive
```

### Backend Support

The parser converts Lucene queries to multiple backend formats with best-effort support:

#### SQL (PostgreSQL, MySQL, SQLite)
- Converts to parameterized WHERE clauses
- Supports ILIKE for case-insensitive matching
- Implements range queries with `>=`, `<=`, `>`, `<` operators
- JSONB fields use `->>'key'` syntax for PostgreSQL

#### DynamoDB PartiQL
- Converts to PartiQL query syntax
- Uses `begins_with()` for prefix wildcards
- Uses `contains()` for suffix and contains wildcards
- Supports range comparisons

#### Map Format
- Abstract representation for custom backends
- Preserves all query semantics in a structured format

## Usage

### Basic Usage

```go
package main

import (
    "github.com/tink3rlabs/magic/storage/search/lucene"
)

func main() {
    // Define searchable fields
    fields := []lucene.FieldInfo{
        {Name: "name", IsJSONB: false},
        {Name: "email", IsJSONB: false},
        {Name: "age", IsJSONB: false},
        {Name: "metadata", IsJSONB: true},
    }

    // Create parser
    parser := lucene.NewParser(fields)

    // Parse to SQL
    sql, values, err := parser.ParseToSQL(`name:john* AND age:[25 TO 65]`)
    // Output: "name::text ILIKE ? AND (age >= ? AND age <= ?)", ["john%", 25, 65]
}
```

### Auto-detect Fields from Struct

```go
type User struct {
    Name     string `json:"name"`
    Email    string `json:"email"`
    Age      int    `json:"age"`
    Metadata string `json:"metadata" gorm:"type:jsonb"`
}

parser, err := lucene.NewParserFromType(User{})
```

### Parse to Different Formats

```go
// Parse to SQL
sql, values, err := parser.ParseToSQL(query)

// Parse to DynamoDB PartiQL
partiql, attributeValues, err := parser.ParseToDynamoDBPartiQL(query)

// Parse to Map (for custom backends)
queryMap, err := parser.ParseToMap(query)
```

## Query Examples

### Simple Queries
```
name:john                          # Exact match
name:john*                         # Prefix wildcard
email:*@example.com                # Suffix wildcard
```

### Boolean Combinations
```
name:john AND status:active
name:john OR name:jane
name:john NOT status:inactive
name:john && age:[25 TO *]
(name:john || email:john@*) && status:active
```

### Range Queries
```
age:[18 TO 65]                     # Ages 18-65 inclusive
price:{100 TO 500}                 # Price between 100-500 exclusive
created_at:[2024-01-01 TO *]       # Created after 2024-01-01
score:[* TO 100]                   # Score up to 100
```

### Quoted Phrases
```
description:"Apache Lucene"
title:"Query Parser Syntax"
```

### Complex Nested Queries
```
(name:john* OR email:*@example.com) AND (status:active OR status:pending) AND age:[25 TO 65]
```

### JSONB Queries
```
metadata.role:admin
settings.theme:dark
config.enabled:true
```

## Performance

The parser uses a custom lexer and recursive descent parser for efficient query parsing:

- **Zero allocations** for simple queries
- **Optimized tokenization** with lookahead
- **Lazy evaluation** of logical operators
- **Best-effort SQL generation** with minimal overhead

Benchmark results (example):
```
BenchmarkParser-8    100000    10523 ns/op    2048 B/op    32 allocs/op
```

## Limitations & Known Issues

1. **Escaping**: Character escaping with backslash (`\+`, `\-`) is partially implemented
2. **Fuzzy Search**: `term~2` syntax is parsed but converted to wildcard matching (best-effort)
3. **Proximity Search**: `"term1 term2"~5` is parsed but treated as exact phrase match
4. **Boosting**: `term^4` is parsed but ignored (no relevance scoring in SQL/DynamoDB)
5. **Prohibited in Mixed Context**: The `-` operator in complex queries may not always produce NOT in SQL

## Architecture

The parser consists of three main components:

1. **Lexer** (`lexer.go`): Tokenizes input into Lucene query tokens
2. **Parser** (`parser_new.go`): Builds an Abstract Syntax Tree (AST) from tokens
3. **Code Generators**: Convert AST to SQL, PartiQL, or Map format

### AST Node Types

- `NodeTerm`: Field-value equality
- `NodeWildcard`: Wildcard pattern matching
- `NodeLogical`: Boolean operations (AND, OR, NOT)
- `NodeRange`: Range queries (via `RangeInfo`)

### Automatic Parser Selection

The parser automatically selects the enhanced parser when detecting advanced syntax:
- Alternative operators (`&&`, `||`, `!`)
- Range queries (`[`, `{`, `TO`)
- Modifiers (`+`, `-`, `~`, `^`)

Legacy queries continue to use the original parser for backward compatibility.

## Testing

Run the comprehensive test suite:

```bash
go test ./storage/search/lucene/...
```

Tests cover:
- Basic field searches
- Wildcard patterns
- Boolean operators (both forms)
- Range queries (inclusive/exclusive)
- Quoted phrases
- JSONB field access
- Complex nested queries
- Implicit searches
- Lexer tokenization

## Future Enhancements

- Full escape sequence support
- Native fuzzy matching (Levenshtein distance)
- Proximity search approximation
- Query optimization and rewriting
- Support for more backends (Elasticsearch, MongoDB, etc.)
- Query validation and suggestions

## References

- [Apache Lucene Query Parser Syntax](https://lucene.apache.org/core/2_9_4/queryparsersyntax.html)
- [Standard Query Parser](https://lucene.apache.org/core/9_9_0/queryparser/org/apache/lucene/queryparser/flexible/standard/StandardQueryParser.html)
