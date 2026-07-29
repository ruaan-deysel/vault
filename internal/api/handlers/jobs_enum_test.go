package handlers

import (
	"testing"

	"github.com/ruaan-deysel/vault/internal/config"
)

// TestValidateJobEnumContainerMode pins the container_mode allow-list to the
// values the rest of the stack actually uses. The API list carried
// "all_at_once" while the UI, the runner and config.ContainerStopAll all use
// "stop_all", so saving a job in Batch mode was rejected outright (issue #261).
func TestValidateJobEnumContainerMode(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{string(config.ContainerOneByOne), false},
		{string(config.ContainerStopAll), false},
		{"", false}, // omitted — the DB column default applies
		{"nonsense", true},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			err := validateJobEnum("container_mode", tc.value)
			if tc.wantErr && err == nil {
				t.Fatalf("validateJobEnum(container_mode, %q) = nil, want error", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateJobEnum(container_mode, %q) = %v, want nil", tc.value, err)
			}
		})
	}
}

// TestNormalizeJobEnumLegacyAlias pins the repair path for jobs persisted by a
// writer that skips this validator (MCP, import, replication). Rejecting the
// stored value outright would leave such a job uneditable — even toggling
// Enabled sends the whole job back through validation.
func TestNormalizeJobEnumLegacyAlias(t *testing.T) {
	got := normalizeJobEnum("container_mode", "all_at_once")
	if got != string(config.ContainerStopAll) {
		t.Fatalf("normalizeJobEnum(container_mode, all_at_once) = %q, want %q", got, config.ContainerStopAll)
	}
	if err := validateJobEnum("container_mode", got); err != nil {
		t.Fatalf("normalised value must validate: %v", err)
	}
	// Values with no alias are passed through untouched.
	for _, v := range []string{string(config.ContainerOneByOne), string(config.ContainerStopAll), "nonsense", ""} {
		if out := normalizeJobEnum("container_mode", v); out != v {
			t.Errorf("normalizeJobEnum(container_mode, %q) = %q, want it unchanged", v, out)
		}
	}
}

// TestJobEnumsMatchConfigConstants guards the whole allow-list against the same
// class of drift: every value the config package declares as a valid mode must
// be accepted by the API validator.
func TestJobEnumsMatchConfigConstants(t *testing.T) {
	for field, want := range map[string][]string{
		"container_mode": {string(config.ContainerOneByOne), string(config.ContainerStopAll)},
	} {
		for _, v := range want {
			if err := validateJobEnum(field, v); err != nil {
				t.Errorf("%s: config declares %q but the API validator rejects it: %v", field, v, err)
			}
		}
	}
}
