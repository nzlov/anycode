package mindmap

import (
	"slices"
	"strings"
	"testing"
)

func TestCompileSearchQueryUsesSQLBooleanPrecedenceAndParentheses(t *testing.T) {
	tests := []struct {
		query string
		value string
		want  bool
	}{
		{query: "alpha OR beta AND gamma", value: "alpha", want: true},
		{query: "alpha OR beta AND gamma", value: "beta", want: false},
		{query: "alpha OR beta AND gamma", value: "beta gamma", want: true},
		{query: "(ALPHA or beta) aNd GAMMA", value: "alpha", want: false},
		{query: "(ALPHA or beta) aNd GAMMA", value: "alpha gamma", want: true},
		{query: "alpha AND(beta OR gamma)", value: "alpha gamma", want: true},
	}

	for _, test := range tests {
		compiled, err := compileSearchQuery(test.query)
		if err != nil {
			t.Fatalf("compileSearchQuery(%q): %v", test.query, err)
		}
		if got := compiled.root.matches(strings.ToLower(test.value)); got != test.want {
			t.Fatalf("compileSearchQuery(%q).matches(%q) = %t, want %t", test.query, test.value, got, test.want)
		}
	}
}

func TestCompileSearchQueryRejectsInvalidSyntax(t *testing.T) {
	for _, query := range []string{"", "alpha beta", "alpha AND", "(alpha OR beta", "alpha OR OR beta", "()"} {
		if _, err := compileSearchQuery(query); err == nil || !strings.Contains(err.Error(), "syntax error") {
			t.Fatalf("compileSearchQuery(%q) error = %v", query, err)
		}
	}
}

func TestSearchGraphWithQueryAppliesCompiledExpression(t *testing.T) {
	graph := GraphDTO{Nodes: []NodeDTO{
		{ID: "n1", Title: "Alpha"},
		{ID: "n2", Title: "Beta", Content: "Gamma"},
		{ID: "n3", Title: "Beta"},
		{ID: "n4", Title: "Alpha", Content: "Gamma"},
	}}
	query := "(alpha OR beta) AND gamma"
	compiled, err := compileSearchQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	result := searchGraphWithQuery(graph, query, 5, compiled.terms, "", func(value string) bool {
		return compiled.root.matches(value)
	})
	ids := make([]string, 0, len(result.Matches))
	for _, match := range result.Matches {
		ids = append(ids, string(match.Node.ID))
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"n2", "n4"}) {
		t.Fatalf("matches = %#v", result.Matches)
	}
}
