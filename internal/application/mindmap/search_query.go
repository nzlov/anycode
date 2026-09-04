package mindmap

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type searchQueryTokenKind uint8

const (
	searchQueryTerm searchQueryTokenKind = iota
	searchQueryAnd
	searchQueryOr
	searchQueryLeftParen
	searchQueryRightParen
)

type searchQueryToken struct {
	kind  searchQueryTokenKind
	value string
}

type searchExpression struct {
	kind        searchQueryTokenKind
	term        string
	left, right *searchExpression
}

func (e *searchExpression) matches(value string) bool {
	switch e.kind {
	case searchQueryTerm:
		return strings.Contains(value, e.term)
	case searchQueryAnd:
		return e.left.matches(value) && e.right.matches(value)
	case searchQueryOr:
		return e.left.matches(value) || e.right.matches(value)
	default:
		return false
	}
}

type compiledSearchQuery struct {
	root  *searchExpression
	terms []string
}

func compileSearchQuery(query string) (compiledSearchQuery, error) {
	tokens := tokenizeSearchQuery(query)
	parser := searchQueryParser{tokens: tokens}
	root, err := parser.parseOr()
	if err != nil {
		return compiledSearchQuery{}, err
	}
	if parser.index != len(tokens) {
		return compiledSearchQuery{}, fmt.Errorf("mind map search query syntax error near %q: expected AND or OR", tokens[parser.index].value)
	}
	terms := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token.kind != searchQueryTerm {
			continue
		}
		if _, exists := seen[token.value]; exists {
			continue
		}
		seen[token.value] = struct{}{}
		terms = append(terms, token.value)
	}
	return compiledSearchQuery{root: root, terms: terms}, nil
}

func tokenizeSearchQuery(query string) []searchQueryToken {
	tokens := make([]searchQueryToken, 0)
	for index := 0; index < len(query); {
		r, size := utf8.DecodeRuneInString(query[index:])
		if unicode.IsSpace(r) {
			index += size
			continue
		}
		switch r {
		case '(':
			tokens = append(tokens, searchQueryToken{kind: searchQueryLeftParen, value: "("})
			index += size
			continue
		case ')':
			tokens = append(tokens, searchQueryToken{kind: searchQueryRightParen, value: ")"})
			index += size
			continue
		}
		start := index
		for index < len(query) {
			r, size = utf8.DecodeRuneInString(query[index:])
			if unicode.IsSpace(r) || r == '(' || r == ')' {
				break
			}
			index += size
		}
		value := strings.ToLower(query[start:index])
		kind := searchQueryTerm
		switch strings.ToUpper(value) {
		case "AND":
			kind = searchQueryAnd
		case "OR":
			kind = searchQueryOr
		}
		tokens = append(tokens, searchQueryToken{kind: kind, value: value})
	}
	return tokens
}

type searchQueryParser struct {
	tokens []searchQueryToken
	index  int
}

func (p *searchQueryParser) parseOr() (*searchExpression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.match(searchQueryOr) {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &searchExpression{kind: searchQueryOr, left: left, right: right}
	}
	return left, nil
}

func (p *searchQueryParser) parseAnd() (*searchExpression, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.match(searchQueryAnd) {
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = &searchExpression{kind: searchQueryAnd, left: left, right: right}
	}
	return left, nil
}

func (p *searchQueryParser) parsePrimary() (*searchExpression, error) {
	if p.index == len(p.tokens) {
		return nil, fmt.Errorf("mind map search query syntax error at end: expected a term or (")
	}
	token := p.tokens[p.index]
	if token.kind == searchQueryTerm {
		p.index++
		return &searchExpression{kind: searchQueryTerm, term: token.value}, nil
	}
	if token.kind != searchQueryLeftParen {
		return nil, fmt.Errorf("mind map search query syntax error near %q: expected a term or (", token.value)
	}
	p.index++
	expression, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.match(searchQueryRightParen) {
		return nil, fmt.Errorf("mind map search query syntax error: expected )")
	}
	return expression, nil
}

func (p *searchQueryParser) match(kind searchQueryTokenKind) bool {
	if p.index == len(p.tokens) || p.tokens[p.index].kind != kind {
		return false
	}
	p.index++
	return true
}
