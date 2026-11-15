package codexcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionLogByIDCanonicalAndLegacyNames(t *testing.T) {
	for _, name := range []string{"rollout-2026-11-14T01-02-03-thread-target.jsonl", "rollout-thread-target.jsonl", "renamed.jsonl"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, "sessions", "2026", "11", "14")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(dir, "rollout-2026-11-14T01-02-03-thread-other.jsonl")
			if err := os.WriteFile(unrelated, []byte(`{"type":"session_meta","payload":{"id":"thread-other"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, name)
			if err := os.WriteFile(target, []byte(`{"type":"session_meta","payload":{"session_id":"thread-target"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := sessionLogByID(home, "thread-target")
			if err != nil || got != target {
				t.Fatalf("lookup = %q, %v", got, err)
			}
			got, err = sessionLogByID(home, "missing")
			if err != nil || got != "" {
				t.Fatalf("missing = %q, %v", got, err)
			}
		})
	}
}

func TestSessionLogByIDVerifiesCanonicalMetadata(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-11-14T01-02-03-target.jsonl"), []byte(`{"type":"session_meta","payload":{"id":"other"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := sessionLogByID(home, "target")
	if err != nil || got != "" {
		t.Fatalf("mismatched metadata = %q, %v", got, err)
	}
}

func BenchmarkSessionLogByID(b *testing.B) {
	home := b.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "14")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		b.Fatal(err)
	}
	for i := range 1000 {
		id := fmt.Sprintf("thread-%04d", i)
		body := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"instructions":%q}}`, id, strings.Repeat("x", 8192))
		if err := os.WriteFile(filepath.Join(dir, "rollout-2026-11-14T01-02-03-"+id+".jsonl"), []byte(body), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sessionLogByID(home, "thread-0999"); err != nil {
			b.Fatal(err)
		}
	}
}
