package web

import (
	"strings"
	"testing"
)

func TestDashboardIncludesExplicitProfileDialog(t *testing.T) {
	payload, err := Assets.ReadFile("index.html")
	if err != nil {
		t.Fatalf("read embedded dashboard: %v", err)
	}
	html := string(payload)

	for _, required := range []string{
		"id=\"profileDialogBackdrop\"",
		"id=\"dialogPassword\"",
		"Save &amp; Scan Now",
		"openProfileDialog",
		"stored - leave blank to keep",
		"profileSecretStatus",
		"secret:",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("dashboard missing %q", required)
		}
	}
}
