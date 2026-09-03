package worker

import (
	"sort"
	"testing"
)

func TestTierRegistry(t *testing.T) {
	expectedTiers := map[string]struct {
		model  string
		effort string
	}{
		"default": {model: "9router/ag/gemini-3.8-flash-high", effort: "max"},
		"orbit":   {model: "9router/ag/gemini-3.8-flash-high", effort: "max"},
		"flash":   {model: "9router/ag/gemini-3.8-flash-high", effort: "max"},
		"gemini":  {model: "9router/ag/gemini-3.8-flash-high", effort: "max"},
		"strato":  {model: "9router/cx/gpt-5.6-luna", effort: "max"},
		"luna":    {model: "9router/cx/gpt-5.6-luna", effort: "max"},
		"gpt":     {model: "9router/cx/gpt-5.6-luna", effort: "max"},
	}

	for name, exp := range expectedTiers {
		if !IsKnownTier(name) {
			t.Errorf("expected %q to be a known tier", name)
		}
		tier, err := GetTier(name)
		if err != nil {
			t.Errorf("GetTier(%q) error = %v", name, err)
			continue
		}
		if tier.Name != name {
			t.Errorf("GetTier(%q).Name = %q, want %q", name, tier.Name, name)
		}
		if tier.Model != exp.model {
			t.Errorf("GetTier(%q).Model = %q, want %q", name, tier.Model, exp.model)
		}
		if tier.Effort != exp.effort {
			t.Errorf("GetTier(%q).Effort = %q, want %q", name, tier.Effort, exp.effort)
		}
	}

	if IsKnownTier("terra") {
		t.Errorf("terra should not be a known tier")
	}

	if _, err := GetTier("terra"); err == nil {
		t.Errorf("GetTier('terra') should return error")
	}

	names := TierNames()
	sort.Strings(names)
	wantNames := []string{"default", "flash", "gemini", "gpt", "luna", "orbit", "strato"}
	if len(names) != len(wantNames) {
		t.Fatalf("TierNames() length = %d, want %d", len(names), len(wantNames))
	}
	for i := range wantNames {
		if names[i] != wantNames[i] {
			t.Errorf("TierNames()[%d] = %q, want %q", i, names[i], wantNames[i])
		}
	}
}
