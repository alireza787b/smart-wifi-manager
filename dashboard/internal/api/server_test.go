package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMetaIncludesConfiguredPaths(t *testing.T) {
	t.Parallel()

	server := NewServer(Options{
		ConfigPath: "/etc/smart-wifi-manager/config.json",
		StatusPath: "/run/smart-wifi-manager/status.json",
		ControlDir: "/var/lib/smart-wifi-manager/control",
		LogPath:    "/var/log/smart-wifi-manager/smart-wifi-manager.log",
		Version:    "test-version",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal meta payload: %v", err)
	}

	if payload["version"] != "test-version" {
		t.Fatalf("unexpected version: %#v", payload["version"])
	}
	if payload["config_path"] != "/etc/smart-wifi-manager/config.json" {
		t.Fatalf("unexpected config_path: %#v", payload["config_path"])
	}
	if payload["status_path"] != "/run/smart-wifi-manager/status.json" {
		t.Fatalf("unexpected status_path: %#v", payload["status_path"])
	}
}

func TestStatusFallsBackWhenStatusFileMissing(t *testing.T) {
	t.Parallel()

	server := NewServer(Options{
		StatusPath: filepath.Join(t.TempDir(), "missing-status.json"),
		Version:    "test-version",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal status payload: %v", err)
	}

	warnings, ok := payload["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected warnings in fallback payload, got %#v", payload["warnings"])
	}
}

func TestStatusPassesThroughFilePayload(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(statusPath, []byte(`{"hostname":"node-01","warnings":[]}`), 0o600); err != nil {
		t.Fatalf("write status file: %v", err)
	}

	server := NewServer(Options{StatusPath: statusPath})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"hostname\":\"node-01\",\"warnings\":[]}" {
		t.Fatalf("unexpected status payload: %q", got)
	}
}
