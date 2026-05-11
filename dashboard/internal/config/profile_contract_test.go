package config

import "testing"

func TestProfileDiffFleetMergeOutdatedWithBaselineChangesAndLocalExtra(t *testing.T) {
	local := Default()
	local.Profiles = []Profile{
		{ID: "fleet", SSID: "Fleet", Priority: 10},
		{ID: "local", SSID: "LocalEmergency", Priority: 10},
	}
	baseline := Default()
	baseline.Profiles = []Profile{{ID: "fleet", SSID: "Fleet", Priority: 100}}

	diff, err := ProfileDiff(local, baseline, "fleet-merge")
	if err != nil {
		t.Fatalf("ProfileDiff: %v", err)
	}
	if diff["drift_state"] != "outdated" {
		t.Fatalf("expected outdated, got %#v", diff["drift_state"])
	}
}

func TestMergePreservesPasswordFileWhenBaselineIsSanitized(t *testing.T) {
	local := Default()
	local.Profiles = []Profile{{ID: "field", SSID: "Field", Priority: 100, PasswordFile: "/run/secrets/field.pass"}}
	baseline := Default()
	baseline.Profiles = []Profile{{ID: "field", SSID: "Field", Priority: 110}}

	merged := Merge(local, baseline, false)

	if len(merged.Profiles) != 1 {
		t.Fatalf("expected one profile, got %d", len(merged.Profiles))
	}
	if merged.Profiles[0].PasswordFile != "/run/secrets/field.pass" {
		t.Fatalf("password file was not preserved: %#v", merged.Profiles[0].PasswordFile)
	}
}
