package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

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
}

func NewServer(options Options) *Server {
	return &Server{options: options}
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
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
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
