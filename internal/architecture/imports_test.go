package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var forbiddenDomainImports = []string{
	"github.com/99designs/gqlgen",
	"github.com/nzlov/anycode/internal/infra",
	"github.com/nzlov/anycode/internal/interfaces",
	"entgo.io/ent",
	"net/http",
	"os/exec",
}

var forbiddenApplicationImports = []string{
	"github.com/99designs/gqlgen",
	"github.com/nzlov/anycode/internal/infra",
	"github.com/nzlov/anycode/internal/interfaces",
	"entgo.io/ent",
	"net/http",
	"os/exec",
}

func TestDomainLayerImports(t *testing.T) {
	assertNoForbiddenImports(t, filepath.Join("..", "domain"), forbiddenDomainImports)
}

func TestApplicationLayerImports(t *testing.T) {
	assertNoForbiddenImports(t, filepath.Join("..", "application"), forbiddenApplicationImports)
}

func TestGeneratedCodeTargets(t *testing.T) {
	assertDir(t, filepath.Join("..", "infra", "entstore", "ent", "schema"))
	assertDir(t, filepath.Join("..", "interfaces", "graphql", "graph"))
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected directory %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", path)
	}
}

func assertNoForbiddenImports(t *testing.T, root string, forbidden []string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			value, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			for _, prefix := range forbidden {
				if value == prefix || strings.HasPrefix(value, prefix+"/") {
					t.Fatalf("%s imports forbidden dependency %s", path, value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
