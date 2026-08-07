package application

import "testing"

func TestNormalizeSuggestedLinksSettings(t *testing.T) {
	got := normalizeSuggestedLinksSettings(SuggestedLinksSettings{})
	if got.Mode != "off" || got.Trigger != "onSave" || got.Placement != "relatedSection" || got.MinScore != 0.45 || got.MaxSuggestions != 5 {
		t.Fatalf("unexpected defaults: %#v", got)
	}

	got = normalizeSuggestedLinksSettings(SuggestedLinksSettings{
		Mode: "automatic", Trigger: "whileEditing", Placement: "preferInline",
		MinScore: 0.7, MaxSuggestions: 99,
	})
	if got.Mode != "automatic" || got.Trigger != "whileEditing" || got.Placement != "preferInline" || got.MinScore != 0.7 || got.MaxSuggestions != 10 {
		t.Fatalf("unexpected normalized settings: %#v", got)
	}
}
