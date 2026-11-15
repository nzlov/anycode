package filestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nzlov/anycode/internal/domain/session"
)

func TestFindSessionFileLegacyLayouts(t *testing.T) {
	for _, relative := range []string{"session-1/target/image.png", "session-1/requirement/c291cmNl/target/image.png", "session-1/requirement/c291cmNl/upload/target/image.png"} {
		t.Run(relative, func(t *testing.T) {
			store := New(t.TempDir())
			target := filepath.Join(store.attachmentsRoot(), "sessions", filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("image"), 0o600); err != nil {
				t.Fatal(err)
			}
			found, err := store.FindSessionFile(context.Background(), "target")
			if err != nil || found.Path != target || found.SessionID != "session-1" {
				t.Fatalf("found = %#v, %v", found, err)
			}
			for _, id := range []session.SessionFileID{"missing", "../target", "target/image.png"} {
				if _, err := store.FindSessionFile(context.Background(), id); !errors.Is(err, session.ErrSessionFileNotFound) {
					t.Fatalf("id %q: %v", id, err)
				}
			}
		})
	}
}

func BenchmarkFindSessionFile(b *testing.B) {
	store := New(b.TempDir())
	root := filepath.Join(store.attachmentsRoot(), "sessions", "session-1")
	for i := range 1000 {
		dir := filepath.Join(root, fmt.Sprintf("file-%04d", i))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "image.png"), []byte("image"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.FindSessionFile(context.Background(), "file-0999"); err != nil {
			b.Fatal(err)
		}
	}
}
