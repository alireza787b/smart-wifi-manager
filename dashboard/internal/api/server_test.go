package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func serveLocal(server *Server, rec *httptest.ResponseRecorder, req *http.Request) {
	req.RemoteAddr = "127.0.0.1:12345"
	server.Router().ServeHTTP(rec, req)
}

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
	serveLocal(server, rec, req)

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

func TestProfileSummaryUsesRedactedContract(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	statusPath := filepath.Join(dir, "status.json")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"mode":"manage","profiles":[{"id":"field","ssid":"DemoField","priority":100,"password":"supersecret"}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(statusPath, []byte(`{"current_connection":{"ssid":"DemoField"},"warnings":[]}`), 0o600); err != nil {
		t.Fatalf("write status: %v", err)
	}

	server := NewServer(Options{ConfigPath: configPath, StatusPath: statusPath})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/profiles/summary", nil)
	rec := httptest.NewRecorder()
	serveLocal(server, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if payload["schema"] != "mds.sidecar_profile.v1" {
		t.Fatalf("unexpected schema: %#v", payload["schema"])
	}
	if payload["secret_status"] != "stored" {
		t.Fatalf("unexpected secret_status: %#v", payload["secret_status"])
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("supersecret")) {
		t.Fatalf("summary leaked inline password: %s", rec.Body.String())
	}
}

func TestProfileImportDryRunThenApply(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	controlDir := filepath.Join(dir, "control")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"mode":"manage","profiles":[{"id":"local","ssid":"LocalEmergency","priority":10}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	server := NewServer(Options{ConfigPath: configPath, ControlDir: controlDir})
	body := []byte(`{"mode":"fleet-merge","dry_run":true,"baseline":{"version":1,"mode":"manage","profiles":[{"id":"fleet","ssid":"Fleet","priority":100}]}}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/profiles/import", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var plan map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	dryRunID, _ := plan["dry_run_id"].(string)
	token, _ := plan["confirmation_token"].(string)
	if dryRunID == "" || token == "" {
		t.Fatalf("missing dry-run id/token: %#v", plan)
	}

	applyBody, _ := json.Marshal(map[string]any{
		"dry_run_id": dryRunID,
		"confirmation": map[string]any{
			"token":              token,
			"acknowledged_risks": true,
			"operator":           "test",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/profiles/apply", bytes.NewReader(applyBody))
	rec = httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if !bytes.Contains(updated, []byte("Fleet")) || !bytes.Contains(updated, []byte("LocalEmergency")) {
		t.Fatalf("fleet-merge did not preserve expected profiles: %s", string(updated))
	}
	if _, err := os.Stat(filepath.Join(dir, "audit", "profile-control.jsonl")); err != nil {
		t.Fatalf("expected audit log: %v", err)
	}
}

func TestProfileApplyAcceptsConfirmationTokenAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	controlDir := filepath.Join(dir, "control")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"mode":"manage","profiles":[{"id":"local","ssid":"LocalEmergency","priority":10}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	server := NewServer(Options{ConfigPath: configPath, ControlDir: controlDir})
	body := []byte(`{"mode":"fleet-merge","dry_run":true,"baseline":{"version":1,"mode":"manage","profiles":[{"id":"fleet","ssid":"Fleet","priority":100}]}}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/profiles/import", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var plan map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}

	applyBody, _ := json.Marshal(map[string]any{
		"dry_run_id": plan["dry_run_id"],
		"confirmation": map[string]any{
			"confirmation_token": plan["confirmation_token"],
			"acknowledged_risks": true,
			"operator":           "test",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/profiles/apply", bytes.NewReader(applyBody))
	rec = httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProfileImportRejectsRemoteWithoutToken(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"mode":"manage","profiles":[{"id":"local","ssid":"LocalEmergency","priority":10}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	server := NewServer(Options{ConfigPath: configPath, ControlDir: filepath.Join(dir, "control")})
	body := []byte(`{"mode":"fleet-merge","dry_run":true,"baseline":{"version":1,"mode":"manage","profiles":[{"id":"fleet","ssid":"Fleet","priority":100}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/profiles/import", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.2:12345"
	rec := httptest.NewRecorder()

	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProfileImportAllowsRemoteWithToken(t *testing.T) {
	t.Setenv(dashboardTokenEnv, "token-123")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"mode":"manage","profiles":[{"id":"local","ssid":"LocalEmergency","priority":10}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	server := NewServer(Options{ConfigPath: configPath, ControlDir: filepath.Join(dir, "control")})
	body := []byte(`{"mode":"fleet-merge","dry_run":true,"baseline":{"version":1,"mode":"manage","profiles":[{"id":"fleet","ssid":"Fleet","priority":100}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/profiles/import", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.2:12345"
	req.Header.Set("Authorization", "Bearer token-123")
	rec := httptest.NewRecorder()

	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProfileApplyRejectsStrictWithoutAdvancedAck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"mode":"manage","profiles":[{"id":"local","ssid":"LocalEmergency","priority":10}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	server := NewServer(Options{ConfigPath: configPath, ControlDir: filepath.Join(dir, "control")})
	body := []byte(`{"mode":"fleet-strict","dry_run":true,"baseline":{"version":1,"mode":"manage","profiles":[{"id":"fleet","ssid":"Fleet","priority":100}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/profiles/import", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected dry-run 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var plan map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	applyBody, _ := json.Marshal(map[string]any{
		"dry_run_id": plan["dry_run_id"],
		"confirmation": map[string]any{
			"token":              plan["confirmation_token"],
			"acknowledged_risks": true,
			"operator":           "test",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/profiles/apply", bytes.NewReader(applyBody))
	rec = httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without advanced ack, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProfileApplyRejectsObserveNoMutationPlan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"mode":"manage","profiles":[{"id":"local","ssid":"LocalEmergency","priority":10}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	server := NewServer(Options{ConfigPath: configPath, ControlDir: filepath.Join(dir, "control")})
	body := []byte(`{"mode":"observe","dry_run":true,"baseline":{"version":1,"mode":"manage","profiles":[{"id":"fleet","ssid":"Fleet","priority":100}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/profiles/import", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected dry-run 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var plan map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	applyBody, _ := json.Marshal(map[string]any{
		"dry_run_id": plan["dry_run_id"],
		"confirmation": map[string]any{
			"token":              plan["confirmation_token"],
			"acknowledged_risks": true,
			"operator":           "test",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/profiles/apply", bytes.NewReader(applyBody))
	rec = httptest.NewRecorder()
	serveLocal(server, rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for observe apply, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLogsRedactSecretArguments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "smart-wifi.log")
	if err := os.WriteFile(logPath, []byte("Executing command: nmcli dev wifi connect FieldNet password 'secret-value' 802-11-wireless-security.psk another-secret\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	server := NewServer(Options{LogPath: logPath})
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	serveLocal(server, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("secret-value")) || bytes.Contains(rec.Body.Bytes(), []byte("another-secret")) {
		t.Fatalf("logs leaked secret values: %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("REDACTED")) {
		t.Fatalf("expected redaction marker in log response: %s", rec.Body.String())
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
	serveLocal(server, rec, req)

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
	serveLocal(server, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"hostname\":\"node-01\",\"warnings\":[]}" {
		t.Fatalf("unexpected status payload: %q", got)
	}
}
