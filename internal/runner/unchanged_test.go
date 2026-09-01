package runner

import (
	"testing"

	"github.com/ruaan-deysel/vault/internal/engine"
)

// TestMarkUnchanged covers the run-log side of issue #326. The flag is
// additive on purpose: status must stay "ok" so nothing that reads it starts
// treating an idle container as a failure.
func TestMarkUnchanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		result *engine.BackupResult
		want   bool
	}{
		{
			name:   "engine reported nothing changed",
			result: &engine.BackupResult{Meta: map[string]any{engine.MetaUnchanged: true}},
			want:   true,
		},
		{
			name:   "engine reported content was captured",
			result: &engine.BackupResult{Meta: map[string]any{engine.MetaUnchanged: false}},
			want:   false,
		},
		{
			name:   "engine said nothing either way",
			result: &engine.BackupResult{Meta: map[string]any{"manifest_id": []byte{0x01}}},
			want:   false,
		},
		{
			name:   "handler set no metadata at all",
			result: &engine.BackupResult{},
			want:   false,
		},
		{
			// A non-boolean under the key is a handler bug, not a licence to
			// mislabel the item — treat it as changed.
			name:   "the flag is not a boolean",
			result: &engine.BackupResult{Meta: map[string]any{engine.MetaUnchanged: "yes"}},
			want:   false,
		},
		{
			// Items that failed before producing a result still get a run-log
			// entry, so nil must not panic.
			name:   "no result at all",
			result: nil,
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resEntry := map[string]any{"name": "plex", "status": "ok"}
			markUnchanged(resEntry, tc.result)
			got, set := resEntry["unchanged"]
			if tc.want {
				if !set || got != true {
					t.Errorf(`resEntry["unchanged"] = %v (set=%v), want true`, got, set)
				}
			} else if set {
				t.Errorf(`resEntry["unchanged"] = %v, want the key to be absent`, got)
			}
			if resEntry["status"] != "ok" {
				t.Errorf(`status = %v, want it left as "ok"`, resEntry["status"])
			}
		})
	}
}
