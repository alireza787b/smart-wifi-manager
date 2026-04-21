package config

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Profile struct {
	ID                string `json:"id"`
	SSID              string `json:"ssid"`
	Priority          int    `json:"priority"`
	ConnectionName    string `json:"connection_name,omitempty"`
	Password          string `json:"password,omitempty"`
	PasswordFile      string `json:"password_file,omitempty"`
	Autoconnect       bool   `json:"autoconnect"`
	Disabled          bool   `json:"disabled"`
	Notes             string `json:"notes,omitempty"`
	HasInlinePassword bool   `json:"has_inline_password,omitempty"`
	ClearInlineSecret bool   `json:"clear_inline_password,omitempty"`
}

type Config struct {
	Version               int       `json:"version"`
	Mode                  string    `json:"mode"`
	Interface             string    `json:"interface"`
	ScanIntervalSec       int       `json:"scan_interval_sec"`
	SignalSwitchThreshold int       `json:"signal_switch_threshold"`
	ConnectTimeoutSec     int       `json:"connect_timeout_sec"`
	CooldownSec           int       `json:"cooldown_sec"`
	AllowOpenNetworks     bool      `json:"allow_open_networks"`
	Profiles              []Profile `json:"profiles"`
}

func Default() Config {
	return Config{
		Version:               1,
		Mode:                  "manage",
		ScanIntervalSec:       10,
		SignalSwitchThreshold: 20,
		ConnectTimeoutSec:     10,
		CooldownSec:           60,
		AllowOpenNetworks:     false,
		Profiles:              []Profile{},
	}
}

func normalizeMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "manage":
		return "manage", nil
	case "observe":
		return "observe", nil
	case "disabled":
		return "disabled", nil
	default:
		return "", errors.New("mode must be manage, observe, or disabled")
	}
}

func normalizeProfile(profile Profile, index int) (Profile, error) {
	profile.SSID = strings.TrimSpace(profile.SSID)
	if profile.SSID == "" {
		return Profile{}, errors.New("profiles[" + strconv.Itoa(index) + "] is missing ssid")
	}
	profile.ID = strings.TrimSpace(profile.ID)
	if profile.ID == "" {
		profile.ID = profile.SSID
	}
	if profile.Priority < 0 {
		profile.Priority = 0
	}
	return profile, nil
}

func Load(path string) (Config, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return Config{}, err
	}

	mode, err := normalizeMode(cfg.Mode)
	if err != nil {
		return Config{}, err
	}
	cfg.Mode = mode
	if cfg.ScanIntervalSec < 2 {
		cfg.ScanIntervalSec = 2
	}
	if cfg.ConnectTimeoutSec < 3 {
		cfg.ConnectTimeoutSec = 3
	}
	if cfg.SignalSwitchThreshold < 0 {
		cfg.SignalSwitchThreshold = 0
	}
	if cfg.CooldownSec < 0 {
		cfg.CooldownSec = 0
	}

	seen := map[string]struct{}{}
	for index, profile := range cfg.Profiles {
		normalized, err := normalizeProfile(profile, index)
		if err != nil {
			return Config{}, err
		}
		if _, exists := seen[normalized.ID]; exists {
			return Config{}, errors.New("duplicate profile id: " + normalized.ID)
		}
		seen[normalized.ID] = struct{}{}
		cfg.Profiles[index] = normalized
	}

	sort.SliceStable(cfg.Profiles, func(i, j int) bool {
		if cfg.Profiles[i].Priority != cfg.Profiles[j].Priority {
			return cfg.Profiles[i].Priority > cfg.Profiles[j].Priority
		}
		return strings.ToLower(cfg.Profiles[i].SSID) < strings.ToLower(cfg.Profiles[j].SSID)
	})

	return cfg, nil
}

func Redacted(cfg Config) Config {
	out := cfg
	out.Profiles = make([]Profile, len(cfg.Profiles))
	copy(out.Profiles, cfg.Profiles)
	for index := range out.Profiles {
		if out.Profiles[index].Password != "" {
			out.Profiles[index].HasInlinePassword = true
			out.Profiles[index].Password = ""
		}
		out.Profiles[index].ClearInlineSecret = false
	}
	return out
}

func Save(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func Merge(existing Config, incoming Config, replace bool) Config {
	if replace {
		return incoming
	}

	merged := existing
	merged.Version = incoming.Version
	if incoming.Mode != "" {
		merged.Mode = incoming.Mode
	}
	if incoming.Interface != "" || incoming.Interface == "" {
		merged.Interface = incoming.Interface
	}
	if incoming.ScanIntervalSec != 0 {
		merged.ScanIntervalSec = incoming.ScanIntervalSec
	}
	if incoming.SignalSwitchThreshold != 0 || incoming.SignalSwitchThreshold == 0 {
		merged.SignalSwitchThreshold = incoming.SignalSwitchThreshold
	}
	if incoming.ConnectTimeoutSec != 0 {
		merged.ConnectTimeoutSec = incoming.ConnectTimeoutSec
	}
	if incoming.CooldownSec != 0 || incoming.CooldownSec == 0 {
		merged.CooldownSec = incoming.CooldownSec
	}
	merged.AllowOpenNetworks = incoming.AllowOpenNetworks

	byID := map[string]Profile{}
	for _, profile := range existing.Profiles {
		byID[profile.ID] = profile
	}
	for _, incomingProfile := range incoming.Profiles {
		existingProfile, exists := byID[incomingProfile.ID]
		if exists && incomingProfile.Password == "" && !incomingProfile.ClearInlineSecret {
			incomingProfile.Password = existingProfile.Password
		}
		if incomingProfile.ClearInlineSecret {
			incomingProfile.Password = ""
			incomingProfile.ClearInlineSecret = false
		}
		byID[incomingProfile.ID] = incomingProfile
	}
	merged.Profiles = merged.Profiles[:0]
	for _, profile := range byID {
		merged.Profiles = append(merged.Profiles, profile)
	}
	sort.SliceStable(merged.Profiles, func(i, j int) bool {
		if merged.Profiles[i].Priority != merged.Profiles[j].Priority {
			return merged.Profiles[i].Priority > merged.Profiles[j].Priority
		}
		return strings.ToLower(merged.Profiles[i].SSID) < strings.ToLower(merged.Profiles[j].SSID)
	})
	return merged
}

func ApplyUpdate(existing Config, incoming Config) Config {
	out := incoming
	existingByID := map[string]Profile{}
	for _, profile := range existing.Profiles {
		existingByID[profile.ID] = profile
	}
	for index, profile := range out.Profiles {
		existingProfile, exists := existingByID[profile.ID]
		if exists && profile.Password == "" && !profile.ClearInlineSecret {
			profile.Password = existingProfile.Password
		}
		if profile.ClearInlineSecret {
			profile.Password = ""
			profile.ClearInlineSecret = false
		}
		out.Profiles[index] = profile
	}
	sort.SliceStable(out.Profiles, func(i, j int) bool {
		if out.Profiles[i].Priority != out.Profiles[j].Priority {
			return out.Profiles[i].Priority > out.Profiles[j].Priority
		}
		return strings.ToLower(out.Profiles[i].SSID) < strings.ToLower(out.Profiles[j].SSID)
	})
	return out
}
