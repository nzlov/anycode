package mcp

import "testing"

func flag(v bool) *bool { return &v }
func TestResolveScopes(t *testing.T) {
	global := []Entry{{Scope: Scope{Kind: "global"}, Name: "docs", Definition: &Definition{Transport: "http", URL: "https://global.example/mcp", Headers: map[string]string{"Authorization": "global-secret"}}, Enabled: flag(true)}}
	project := []Entry{{Scope: Scope{Kind: "project", ID: "p"}, Name: "docs", Definition: &Definition{Transport: "stdio", Command: "project-mcp"}, Enabled: flag(false)}}
	card := []Entry{{Scope: Scope{Kind: "session", ID: "c"}, Name: "docs", Enabled: flag(true)}}
	got := Resolve(global, project, card)
	if len(got) != 1 || !got[0].Enabled || got[0].Definition.Command != "project-mcp" || got[0].Definition.URL != "" || len(got[0].Definition.Headers) != 0 {
		t.Fatalf("whole definition override: %#v", got)
	}
	got = Resolve(global, project, nil)
	if got[0].Enabled || got[0].Overridden {
		t.Fatalf("project disable must not fall back: %#v", got)
	}
	got = Resolve(global, nil, nil)
	if !got[0].Enabled || got[0].Source != "global" || got[0].Overridden {
		t.Fatalf("reset inheritance: %#v", got)
	}
	got = Resolve(nil, nil, card)
	if len(got) != 0 {
		t.Fatalf("orphan switch resurrects service: %#v", got)
	}
}
func TestDefinitionValidation(t *testing.T) {
	for _, d := range []Definition{{Transport: "stdio"}, {Transport: "http", URL: "file:///tmp/x"}, {Transport: "http", URL: "https://user:pass@example.com"}, {Transport: "http", URL: "https://example.com", Command: "x"}, {Transport: "stdio", Command: "x", Env: map[string]string{"bad=key": "x"}}} {
		if d.Validate() == nil {
			t.Fatalf("accepted %#v", d)
		}
	}
}
