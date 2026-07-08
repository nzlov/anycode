package graph

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
)

func diffFieldSelected(ctx context.Context, name string) bool {
	fieldContext := graphql.GetFieldContext(ctx)
	if fieldContext == nil {
		return true
	}
	var fragments ast.FragmentDefinitionList
	if graphql.HasOperationContext(ctx) {
		if operationContext := graphql.GetOperationContext(ctx); operationContext != nil && operationContext.Doc != nil {
			fragments = operationContext.Doc.Fragments
		}
	}
	return selectionSetHasField(fieldContext.Field.Selections, name, fragments, map[string]bool{})
}

func selectionSetHasField(selections ast.SelectionSet, name string, fragments ast.FragmentDefinitionList, visited map[string]bool) bool {
	for _, selection := range selections {
		switch selected := selection.(type) {
		case *ast.Field:
			if selected.Name == name {
				return true
			}
			if selectionSetHasField(selected.SelectionSet, name, fragments, visited) {
				return true
			}
		case *ast.InlineFragment:
			if selectionSetHasField(selected.SelectionSet, name, fragments, visited) {
				return true
			}
		case *ast.FragmentSpread:
			if visited[selected.Name] {
				continue
			}
			fragment := fragments.ForName(selected.Name)
			if fragment == nil {
				continue
			}
			visited[selected.Name] = true
			if selectionSetHasField(fragment.SelectionSet, name, fragments, visited) {
				return true
			}
		}
	}
	return false
}
