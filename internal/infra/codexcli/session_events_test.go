package codexcli

import "testing"

func TestParseSessionLogLineFiltersCanonicalItemLifecycleMirrors(t *testing.T) {
	tests := []string{
		`{"timestamp":"2026-07-22T13:08:01Z","type":"event_msg","payload":{"type":"item_started","thread_id":"thread-1","turn_id":"turn-1","item":{"type":"CommandExecution","id":"exec-1","status":"in_progress"}}}`,
		`{"timestamp":"2026-07-22T13:08:02Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-1","turn_id":"turn-1","item":{"type":"CommandExecution","id":"exec-1","status":"completed","stdout":"passed"}}}`,
	}
	for index, raw := range tests {
		if got := parseSessionLogLine([]byte(raw), "/workspace/project", "rollout.jsonl", int64(index)); len(got) != 0 {
			t.Fatalf("item lifecycle mirror %d events = %#v, want none", index, got)
		}
	}
}
