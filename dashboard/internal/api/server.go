package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alireza787b/smart-wifi-manager/dashboard/internal/config"
	"github.com/alireza787b/smart-wifi-manager/dashboard/internal/platform"
	"github.com/alireza787b/smart-wifi-manager/dashboard/web"
)

type Options struct {
	ConfigPath string
	StatusPath string
	ControlDir string
	LogPath    string
	Version    string
}

type Server struct {
	options Options
	mu      sync.Mutex
	plans   map[string]map[string]any
}

const dashboardTokenEnv = "SMART_WIFI_MANAGER_API_TOKEN"

func NewServer(options Options) *Server {
	return &Server{options: options, plans: map[string]map[string]any{}}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(web.Assets)))
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/config/export", s.handleConfigExport)
	mux.HandleFunc("/api/config/import", s.handleConfigImport)
	mux.HandleFunc("/api/actions/scan", s.handleScan)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/v1/profiles/summary", s.handleProfileSummary)
	mux.HandleFunc("/api/v1/profiles/export", s.handleProfileExport)
	mux.HandleFunc("/api/v1/profiles/validate", s.handleProfileValidate)
	mux.HandleFunc("/api/v1/profiles/diff", s.handleProfileDiff)
	mux.HandleFunc("/api/v1/profiles/import", s.handleProfileImport)
	mux.HandleFunc("/api/v1/profiles/apply", s.handleProfileApply)
	mux.HandleFunc("/api/v1/profiles/promote-reference-draft", s.handleProfilePromoteDraft)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeBody(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("empty request body")
	}
	return json.Unmarshal(body, target)
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requireMutationAccess(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv(dashboardTokenEnv))
	if expected == "" {
		if isLoopbackRemote(r.RemoteAddr) {
			return true
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": dashboardTokenEnv + " is required for remote mutating requests"})
		return false
	}
	supplied := strings.TrimSpace(r.Header.Get("X-SWM-Profile-Token"))
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if supplied == "" && strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		supplied = strings.TrimSpace(auth[7:])
	}
	if supplied == "" || subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid Smart Wi-Fi Manager mutation token"})
		return false
	}
	return true
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     s.options.Version,
		"config_path": s.options.ConfigPath,
		"status_path": s.options.StatusPath,
		"control_dir": s.options.ControlDir,
		"log_path":    s.options.LogPath,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	payload, err := os.ReadFile(s.options.StatusPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"version":  s.options.Version,
			"warnings": []string{"Status file not available yet"},
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.Load(s.options.ConfigPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, config.Redacted(cfg))
	case http.MethodPut:
		if !requireMutationAccess(w, r) {
			return
		}
		existing, err := config.Load(s.options.ConfigPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var incoming config.Config
		if err := decodeBody(r, &incoming); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		merged := config.ApplyUpdate(existing, incoming)
		if err := config.Save(s.options.ConfigPath, merged); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, config.Redacted(merged))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleConfigExport(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load(s.options.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config.RedactedFleet(cfg))
}

func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireMutationAccess(w, r) {
		return
	}
	existing, err := config.Load(s.options.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var incoming config.Config
	if err := decodeBody(r, &incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	replace := mode == "replace"
	merged := config.Merge(existing, incoming, replace)
	if err := config.Save(s.options.ConfigPath, merged); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config.Redacted(merged))
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireMutationAccess(w, r) {
		return
	}
	if err := platform.Touch(s.options.ControlDir, "scan-now"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queued": true})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	lines, err := platform.TailFile(s.options.LogPath, 200)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"log_path": s.options.LogPath,
			"lines":    []string{},
			"warning":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"log_path": s.options.LogPath,
		"lines":    lines,
	})
}

func (s *Server) readStatus() map[string]any {
	payload, err := os.ReadFile(s.options.StatusPath)
	if err != nil {
		return map[string]any{}
	}
	var status map[string]any
	if err := json.Unmarshal(payload, &status); err != nil {
		return map[string]any{}
	}
	return status
}

func (s *Server) handleProfileSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cfg, err := config.Load(s.options.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config.ProfileSummary(cfg, "node-local", s.options.ConfigPath, s.readStatus()))
}

func (s *Server) handleProfileExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cfg, err := config.Load(s.options.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config.RedactedFleet(cfg))
}

func (s *Server) handleProfileValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireMutationAccess(w, r) {
		return
	}
	var incoming config.Config
	if err := decodeBody(r, &incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config.ValidateBundle(incoming))
}

type profileDiffRequest struct {
	Mode     string        `json:"mode"`
	Baseline config.Config `json:"baseline"`
}

func (s *Server) handleProfileDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireMutationAccess(w, r) {
		return
	}
	local, err := config.Load(s.options.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var req profileDiffRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	diff, err := config.ProfileDiff(local, req.Baseline, req.Mode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

type profileImportRequest struct {
	Mode     string        `json:"mode"`
	DryRun   bool          `json:"dry_run"`
	Baseline config.Config `json:"baseline"`
}

func (s *Server) handleProfileImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireMutationAccess(w, r) {
		return
	}
	var req profileImportRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !req.DryRun {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "profile import requires dry_run=true; use apply with confirmation"})
		return
	}
	local, err := config.Load(s.options.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	plan, err := config.DryRunPlan(local, req.Baseline, req.Mode, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	dryRunID, _ := plan["dry_run_id"].(string)
	s.mu.Lock()
	s.plans[dryRunID] = plan
	s.mu.Unlock()
	s.audit("profile-import-dry-run", map[string]any{"dry_run_id": dryRunID, "mode": req.Mode})
	writeJSON(w, http.StatusOK, config.RedactedPlan(plan))
}

type profileApplyRequest struct {
	DryRunID     string `json:"dry_run_id"`
	Confirmation struct {
		Token             string `json:"token"`
		AcknowledgedRisks bool   `json:"acknowledged_risks"`
		AdvancedStrictAck bool   `json:"advanced_strict_ack"`
		Operator          string `json:"operator"`
	} `json:"confirmation"`
}

func (s *Server) handleProfileApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireMutationAccess(w, r) {
		return
	}
	var req profileApplyRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DryRunID == "" || !req.Confirmation.AcknowledgedRisks {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dry_run_id and acknowledged_risks are required"})
		return
	}
	s.mu.Lock()
	plan := s.plans[req.DryRunID]
	s.mu.Unlock()
	if plan == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dry-run plan not found"})
		return
	}
	if plan["confirmation_token"] != req.Confirmation.Token {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirmation token does not match dry-run plan"})
		return
	}
	if plan["requires_advanced_confirmation"] == true && !req.Confirmation.AdvancedStrictAck {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fleet-strict requires advanced confirmation"})
		return
	}
	if plan["mode"] == "observe" || plan["mode"] == "local" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "observe/local dry-runs do not produce apply mutations"})
		return
	}
	candidate, ok := plan["candidate_config"].(config.Config)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "dry-run plan missing candidate config"})
		return
	}
	if err := config.Save(s.options.ConfigPath, candidate); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result := map[string]any{
		"schema":         config.SidecarProfileSchema,
		"backend":        config.BackendName,
		"applied":        true,
		"mode":           plan["mode"],
		"dry_run_id":     req.DryRunID,
		"applied_at":     time.Now().UTC().Format(time.RFC3339),
		"candidate_hash": config.SanitizedHash(candidate),
	}
	s.audit("profile-apply", result)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleProfilePromoteDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireMutationAccess(w, r) {
		return
	}
	cfg, err := config.Load(s.options.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	draft := map[string]any{
		"schema":     config.SidecarProfileSchema,
		"backend":    config.BackendName,
		"kind":       config.ProfileKind,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"profile":    config.RedactedFleet(cfg),
		"summary":    config.ProfileSummary(cfg, "reference-draft", s.options.ConfigPath, s.readStatus()),
	}
	s.audit("profile-promote-reference-draft", map[string]any{"hash": config.SanitizedHash(cfg)})
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) audit(action string, payload map[string]any) {
	if s.options.ControlDir == "" {
		return
	}
	stateDir := filepath.Dir(s.options.ControlDir)
	auditDir := filepath.Join(stateDir, "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		return
	}
	entry := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"backend":   config.BackendName,
		"action":    action,
		"payload":   payload,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(auditDir, "profile-control.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(data, '\n'))
}
