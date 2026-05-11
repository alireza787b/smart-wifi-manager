package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	SidecarProfileSchema = "mds.sidecar_profile.v1"
	BackendName          = "smart-wifi-manager"
	ProfileKind          = "smart-wifi-manager-profile"
	HashSemantics        = "sha256:canonical-sanitized-payload:12"
)

func NormalizePolicyMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		mode = "fleet-merge"
	}
	switch mode {
	case "observe", "local", "fleet-merge", "fleet-strict":
		return mode, nil
	default:
		return "", errors.New("mode must be observe, local, fleet-merge, or fleet-strict")
	}
}

func SecretStatus(profile Profile) string {
	if strings.TrimSpace(profile.PasswordFile) != "" {
		return "external file"
	}
	if profile.Password != "" || profile.HasInlinePassword {
		return "stored"
	}
	return "missing"
}

func dominantSecretStatus(profiles []Profile) string {
	seen := map[string]bool{}
	for _, profile := range profiles {
		seen[SecretStatus(profile)] = true
	}
	for _, candidate := range []string{"external file", "stored", "redacted"} {
		if seen[candidate] {
			return candidate
		}
	}
	return "missing"
}

func SanitizedProfile(profile Profile) map[string]any {
	id := strings.TrimSpace(profile.ID)
	if id == "" {
		id = strings.TrimSpace(profile.SSID)
	}
	return map[string]any{
		"id":              id,
		"ssid":            strings.TrimSpace(profile.SSID),
		"priority":        profile.Priority,
		"connection_name": strings.TrimSpace(profile.ConnectionName),
		"autoconnect":     profile.Autoconnect,
		"disabled":        profile.Disabled,
		"notes":           strings.TrimSpace(profile.Notes),
		"secret_status":   SecretStatus(profile),
	}
}

func sanitizedPayload(cfg Config) map[string]any {
	profiles := make([]map[string]any, 0, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		profiles = append(profiles, SanitizedProfile(profile))
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		leftID, _ := profiles[i]["id"].(string)
		rightID, _ := profiles[j]["id"].(string)
		if strings.ToLower(leftID) != strings.ToLower(rightID) {
			return strings.ToLower(leftID) < strings.ToLower(rightID)
		}
		leftSSID, _ := profiles[i]["ssid"].(string)
		rightSSID, _ := profiles[j]["ssid"].(string)
		return strings.ToLower(leftSSID) < strings.ToLower(rightSSID)
	})
	return map[string]any{
		"version":                 cfg.Version,
		"mode":                    cfg.Mode,
		"interface":               cfg.Interface,
		"scan_interval_sec":       cfg.ScanIntervalSec,
		"signal_switch_threshold": cfg.SignalSwitchThreshold,
		"connect_timeout_sec":     cfg.ConnectTimeoutSec,
		"cooldown_sec":            cfg.CooldownSec,
		"allow_open_networks":     cfg.AllowOpenNetworks,
		"profiles":                profiles,
	}
}

func SanitizedHash(cfg Config) string {
	data, _ := json.Marshal(sanitizedPayload(cfg))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

func RedactedFleet(cfg Config) Config {
	out := Redacted(cfg)
	for index := range out.Profiles {
		out.Profiles[index].PasswordFile = ""
		out.Profiles[index].ClearInlineSecret = false
	}
	return out
}

func ProfileSummary(cfg Config, source string, path string, status map[string]any) map[string]any {
	profiles := make([]map[string]any, 0, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		profiles = append(profiles, SanitizedProfile(profile))
	}
	if status == nil {
		status = map[string]any{}
	}
	return map[string]any{
		"schema":         SidecarProfileSchema,
		"backend":        BackendName,
		"kind":           ProfileKind,
		"source":         source,
		"path":           path,
		"present":        true,
		"hash":           SanitizedHash(cfg),
		"hash_semantics": HashSemantics,
		"profile_count":  len(cfg.Profiles),
		"secret_status":  dominantSecretStatus(cfg.Profiles),
		"profiles":       profiles,
		"runtime": map[string]any{
			"service_mode":       cfg.Mode,
			"current_connection": status["current_connection"],
			"scan":               status["scan"],
			"warnings":           status["warnings"],
		},
	}
}

func profilesByID(cfg Config) map[string]Profile {
	result := map[string]Profile{}
	for _, profile := range cfg.Profiles {
		id := strings.TrimSpace(profile.ID)
		if id == "" {
			id = strings.TrimSpace(profile.SSID)
		}
		if id != "" {
			result[id] = profile
		}
	}
	return result
}

func ProfileDiff(local Config, baseline Config, mode string) (map[string]any, error) {
	normalizedMode, err := NormalizePolicyMode(mode)
	if err != nil {
		return nil, err
	}
	localByID := profilesByID(local)
	baselineByID := profilesByID(baseline)
	added := []string{}
	changed := []string{}
	localExtra := []string{}
	for id, baselineProfile := range baselineByID {
		localProfile, exists := localByID[id]
		if !exists {
			added = append(added, id)
			continue
		}
		if !sanitizedProfilesEqual(localProfile, baselineProfile) {
			changed = append(changed, id)
		}
	}
	for id := range localByID {
		if _, exists := baselineByID[id]; !exists {
			localExtra = append(localExtra, id)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(localExtra)
	driftState := "outdated"
	switch {
	case normalizedMode == "observe" || normalizedMode == "local":
		driftState = "unmanaged"
	case len(baselineByID) == 0 && len(localByID) > 0:
		driftState = "missing_fleet_baseline"
	case len(baselineByID) == 0:
		driftState = "unmanaged"
	case len(added) == 0 && len(changed) == 0 && len(localExtra) == 0:
		driftState = "in_sync"
	case normalizedMode == "fleet-merge" && len(localExtra) > 0 && len(added) == 0 && len(changed) == 0:
		driftState = "local_extra"
	}
	strictPrune := []string{}
	preserveLocal := []string{}
	if normalizedMode == "fleet-strict" {
		strictPrune = append(strictPrune, localExtra...)
	}
	if normalizedMode == "fleet-merge" {
		preserveLocal = append(preserveLocal, localExtra...)
	}
	return map[string]any{
		"schema":        SidecarProfileSchema,
		"backend":       BackendName,
		"mode":          normalizedMode,
		"drift_state":   driftState,
		"local_hash":    SanitizedHash(local),
		"baseline_hash": SanitizedHash(baseline),
		"changes": map[string]any{
			"add_from_baseline":    added,
			"update_from_baseline": changed,
			"local_extra":          localExtra,
			"strict_prune":         strictPrune,
			"preserve_local":       preserveLocal,
		},
		"warnings": diffWarnings(normalizedMode, localExtra),
	}, nil
}

func sanitizedProfilesEqual(a Profile, b Profile) bool {
	left, _ := json.Marshal(SanitizedProfile(a))
	right, _ := json.Marshal(SanitizedProfile(b))
	return string(left) == string(right)
}

func diffWarnings(mode string, localExtra []string) []string {
	warnings := []string{}
	if mode == "observe" {
		warnings = append(warnings, "observe mode reports only and will not apply profile changes")
	}
	if mode == "local" {
		warnings = append(warnings, "local mode keeps the node-local profile authoritative")
	}
	if mode == "fleet-merge" && len(localExtra) > 0 {
		warnings = append(warnings, "fleet-merge preserves local extra profiles and reports drift")
	}
	if mode == "fleet-strict" && len(localExtra) > 0 {
		warnings = append(warnings, "fleet-strict can prune only Smart-Wi-Fi-managed NetworkManager profiles after advanced confirmation")
	}
	return warnings
}

func MergeForPolicyMode(local Config, baseline Config, mode string) (Config, error) {
	normalizedMode, err := NormalizePolicyMode(mode)
	if err != nil {
		return Config{}, err
	}
	if normalizedMode == "observe" || normalizedMode == "local" {
		return local, nil
	}
	if normalizedMode == "fleet-strict" {
		return baseline, nil
	}
	return Merge(local, baseline, false), nil
}

func DryRunPlan(local Config, baseline Config, mode string, includeCandidate bool) (map[string]any, error) {
	candidate, err := MergeForPolicyMode(local, baseline, mode)
	if err != nil {
		return nil, err
	}
	diff, err := ProfileDiff(local, baseline, mode)
	if err != nil {
		return nil, err
	}
	seed := map[string]any{
		"mode":      mode,
		"local":     SanitizedHash(local),
		"baseline":  SanitizedHash(baseline),
		"candidate": SanitizedHash(candidate),
		"created":   time.Now().Unix(),
	}
	seedData, _ := json.Marshal(seed)
	sum := sha256.Sum256(seedData)
	token := hex.EncodeToString(sum[:])[:16]
	plan := map[string]any{
		"schema":                         SidecarProfileSchema,
		"backend":                        BackendName,
		"kind":                           "smart-wifi-manager-profile-plan",
		"dry_run_id":                     "swm-" + token[:12],
		"created_at":                     time.Now().UTC().Format(time.RFC3339),
		"mode":                           mode,
		"confirmation_token":             token,
		"requires_confirmation":          mode != "observe" && mode != "local",
		"requires_advanced_confirmation": mode == "fleet-strict",
		"diff":                           diff,
		"candidate_hash":                 SanitizedHash(candidate),
		"candidate_summary":              ProfileSummary(candidate, "candidate", "", nil),
	}
	if includeCandidate {
		plan["candidate_config"] = candidate
	}
	return plan, nil
}

func RedactedPlan(plan map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range plan {
		out[key] = value
	}
	if candidate, ok := out["candidate_config"].(Config); ok {
		out["candidate_config"] = RedactedFleet(candidate)
	}
	return out
}

func ValidateBundle(cfg Config) map[string]any {
	errorsList := []string{}
	seen := map[string]bool{}
	for index, profile := range cfg.Profiles {
		id := strings.TrimSpace(profile.ID)
		if id == "" {
			id = strings.TrimSpace(profile.SSID)
		}
		if id == "" {
			errorsList = append(errorsList, "profiles entry is missing id or ssid")
		}
		if seen[id] {
			errorsList = append(errorsList, "duplicate profile id: "+id)
		}
		seen[id] = true
		if strings.TrimSpace(profile.SSID) == "" {
			errorsList = append(errorsList, "profiles entry is missing ssid")
		}
		if profile.Priority < 0 {
			errorsList = append(errorsList, "profiles entry priority must be non-negative")
		}
		_ = index
	}
	result := map[string]any{
		"schema":         SidecarProfileSchema,
		"backend":        BackendName,
		"valid":          len(errorsList) == 0,
		"errors":         errorsList,
		"profile_count":  len(cfg.Profiles),
		"hash_semantics": HashSemantics,
	}
	if len(errorsList) == 0 {
		result["hash"] = SanitizedHash(cfg)
	}
	return result
}
