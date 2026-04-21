#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="$SCRIPT_DIR"
CONFIG_PATH="/etc/smart-wifi-manager/config.json"
STATUS_PATH="/run/smart-wifi-manager/status.json"
STATE_DIR="/var/lib/smart-wifi-manager"
LOG_PATH="/var/log/smart-wifi-manager/smart-wifi-manager.log"
DASHBOARD_LISTEN="127.0.0.1:9080"
SKIP_DASHBOARD=false
INSTALL_DASHBOARD_ONLY=false
DASHBOARD_BINARY_PATH="$INSTALL_DIR/build/smart-wifi-manager-dashboard"
DASHBOARD_SERVICE_FILE="/etc/systemd/system/smart-wifi-manager-dashboard.service"
SERVICE_FILE="/etc/systemd/system/smart-wifi-manager.service"

log_info() { echo "[INFO] $1"; }
log_warn() { echo "[WARN] $1"; }
log_error() { echo "[ERROR] $1" >&2; }
log_success() { echo "[OK] $1"; }

usage() {
    cat <<EOF
Smart Wi-Fi Manager installer

Usage: sudo ./install.sh [OPTIONS]

Options:
  --config PATH                Config file path (default: $CONFIG_PATH)
  --status-file PATH           Status file path (default: $STATUS_PATH)
  --state-dir PATH             State directory (default: $STATE_DIR)
  --log-file PATH              Log file path (default: $LOG_PATH)
  --dashboard-listen ADDR      Dashboard listen address (default: $DASHBOARD_LISTEN)
  --skip-dashboard             Install core service only
  --install-dashboard-only     Build/install dashboard service only
  -h, --help                   Show this help text

Examples:
  sudo ./install.sh
  sudo ./install.sh --dashboard-listen 0.0.0.0:9080
  sudo ./install.sh --skip-dashboard
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --config) CONFIG_PATH="$2"; shift 2 ;;
        --status-file) STATUS_PATH="$2"; shift 2 ;;
        --state-dir) STATE_DIR="$2"; shift 2 ;;
        --log-file) LOG_PATH="$2"; shift 2 ;;
        --dashboard-listen) DASHBOARD_LISTEN="$2"; shift 2 ;;
        --skip-dashboard) SKIP_DASHBOARD=true; shift ;;
        --install-dashboard-only) INSTALL_DASHBOARD_ONLY=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *)
            log_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

if [[ "$EUID" -ne 0 ]]; then
    log_error "Run this installer as root."
    exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
    log_error "python3 is required."
    exit 1
fi

if ! command -v nmcli >/dev/null 2>&1; then
    log_warn "nmcli was not found. The service will install, but Wi-Fi management will not work until NetworkManager/nmcli is installed."
fi

render_template() {
    local template_path="$1"
    local output_path="$2"

    sed \
        -e "s|__SWM_INSTALL_DIR__|$INSTALL_DIR|g" \
        -e "s|__SWM_CONFIG_PATH__|$CONFIG_PATH|g" \
        -e "s|__SWM_CONFIG_DIR__|$(dirname "$CONFIG_PATH")|g" \
        -e "s|__SWM_STATUS_PATH__|$STATUS_PATH|g" \
        -e "s|__SWM_STATE_DIR__|$STATE_DIR|g" \
        -e "s|__SWM_RUN_DIR__|$(dirname "$STATUS_PATH")|g" \
        -e "s|__SWM_LOG_PATH__|$LOG_PATH|g" \
        -e "s|__SWM_LOG_DIR__|$(dirname "$LOG_PATH")|g" \
        -e "s|__SWM_DASHBOARD_BINARY__|$DASHBOARD_BINARY_PATH|g" \
        -e "s|__SWM_DASHBOARD_LISTEN__|$DASHBOARD_LISTEN|g" \
        -e "s|__SWM_CONTROL_DIR__|$STATE_DIR/control|g" \
        "$template_path" > "$output_path"
}

ensure_core_paths() {
    mkdir -p "$(dirname "$CONFIG_PATH")" "$(dirname "$STATUS_PATH")" "$STATE_DIR/control" "$(dirname "$LOG_PATH")"
    chmod 700 "$STATE_DIR" "$STATE_DIR/control"

    if [[ ! -f "$CONFIG_PATH" ]]; then
        install -m 600 "$INSTALL_DIR/templates/config.json.template" "$CONFIG_PATH"
        log_success "Created default config at $CONFIG_PATH"
    else
        log_info "Config already exists at $CONFIG_PATH"
    fi
}

build_dashboard() {
    if ! command -v go >/dev/null 2>&1; then
        log_warn "Go is not installed. Skipping dashboard build."
        return 1
    fi

    mkdir -p "$INSTALL_DIR/build"
    log_info "Building dashboard from local source..."
    (
        cd "$INSTALL_DIR/dashboard"
        go build -o "$DASHBOARD_BINARY_PATH" ./cmd
    )
    chmod +x "$DASHBOARD_BINARY_PATH"
    log_success "Dashboard binary ready at $DASHBOARD_BINARY_PATH"
}

install_core_service() {
    render_template "$INSTALL_DIR/smart-wifi-manager.service" "$SERVICE_FILE"
    chmod 644 "$SERVICE_FILE"
    systemctl daemon-reload
    systemctl enable smart-wifi-manager.service >/dev/null 2>&1
    systemctl restart smart-wifi-manager.service
    log_success "smart-wifi-manager.service installed and restarted"
}

install_dashboard_service() {
    if [[ ! -x "$DASHBOARD_BINARY_PATH" ]]; then
        build_dashboard || return 1
    fi

    render_template "$INSTALL_DIR/templates/dashboard.service.template" "$DASHBOARD_SERVICE_FILE"
    chmod 644 "$DASHBOARD_SERVICE_FILE"
    systemctl daemon-reload
    systemctl enable smart-wifi-manager-dashboard.service >/dev/null 2>&1
    systemctl restart smart-wifi-manager-dashboard.service
    log_success "smart-wifi-manager-dashboard.service installed and restarted"
}

if [[ "$INSTALL_DASHBOARD_ONLY" == "true" ]]; then
    install_dashboard_service
    exit 0
fi

chmod +x "$INSTALL_DIR/smart-wifi-manager.sh" "$INSTALL_DIR/smart_wifi_manager.py"
ensure_core_paths
install_core_service

if [[ "$SKIP_DASHBOARD" != "true" ]]; then
    install_dashboard_service || log_warn "Dashboard installation skipped."
fi

log_success "Installation complete."
echo
echo "Core service:      sudo systemctl status smart-wifi-manager.service"
if [[ "$SKIP_DASHBOARD" != "true" ]]; then
    echo "Dashboard:         http://$DASHBOARD_LISTEN"
fi
echo "Configure profiles: sudo ./configure_smart_wifi_manager.sh"
