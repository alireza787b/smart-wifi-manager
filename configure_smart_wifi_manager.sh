#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_PATH="/etc/smart-wifi-manager/config.json"
HEADLESS=false
IMPORT_PATH=""
IMPORT_MODE="merge"
MODE_VALUE=""
INTERFACE_VALUE=""
SCAN_INTERVAL_VALUE=""
SIGNAL_THRESHOLD_VALUE=""
COOLDOWN_VALUE=""
CONNECT_TIMEOUT_VALUE=""
ALLOW_OPEN_NETWORKS_VALUE=""

log_info() { echo "[INFO] $1"; }
log_error() { echo "[ERROR] $1" >&2; }

usage() {
    cat <<EOF
Smart Wi-Fi Manager configuration helper

Usage:
  sudo ./configure_smart_wifi_manager.sh [OPTIONS]

Options:
  --config PATH                  Config file path (default: $CONFIG_PATH)
  --headless                     Do not prompt
  --import PATH                  Import a full config bundle (JSON)
  --import-mode MODE             replace|merge (default: merge)
  --mode VALUE                   manage|observe|disabled
  --interface IFACE              Wi-Fi interface override (blank = auto)
  --scan-interval SEC            Scan interval in seconds
  --signal-threshold VALUE       Minimum signal gain before switching
  --cooldown SEC                 Cooldown after a switch
  --connect-timeout SEC          NetworkManager connection timeout
  --allow-open-networks BOOL     true|false
  -h, --help                     Show help
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --config) CONFIG_PATH="$2"; shift 2 ;;
        --headless) HEADLESS=true; shift ;;
        --import) IMPORT_PATH="$2"; shift 2 ;;
        --import-mode) IMPORT_MODE="$2"; shift 2 ;;
        --mode) MODE_VALUE="$2"; shift 2 ;;
        --interface) INTERFACE_VALUE="$2"; shift 2 ;;
        --scan-interval) SCAN_INTERVAL_VALUE="$2"; shift 2 ;;
        --signal-threshold) SIGNAL_THRESHOLD_VALUE="$2"; shift 2 ;;
        --cooldown) COOLDOWN_VALUE="$2"; shift 2 ;;
        --connect-timeout) CONNECT_TIMEOUT_VALUE="$2"; shift 2 ;;
        --allow-open-networks) ALLOW_OPEN_NETWORKS_VALUE="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *)
            log_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

if [[ "$EUID" -ne 0 ]]; then
    log_error "Run this script as root."
    exit 1
fi

if [[ ! -f "$CONFIG_PATH" ]]; then
    log_error "Config file not found at $CONFIG_PATH. Run sudo ./install.sh first."
    exit 1
fi

if [[ "$HEADLESS" != "true" ]]; then
    [[ -z "$MODE_VALUE" ]] && read -r -p "Mode [manage/observe/disabled] (blank keeps current): " MODE_VALUE
    [[ -z "$INTERFACE_VALUE" ]] && read -r -p "Interface override (blank = auto): " INTERFACE_VALUE
    [[ -z "$SCAN_INTERVAL_VALUE" ]] && read -r -p "Scan interval seconds (blank keeps current): " SCAN_INTERVAL_VALUE
    [[ -z "$SIGNAL_THRESHOLD_VALUE" ]] && read -r -p "Signal threshold (blank keeps current): " SIGNAL_THRESHOLD_VALUE
    [[ -z "$COOLDOWN_VALUE" ]] && read -r -p "Cooldown seconds (blank keeps current): " COOLDOWN_VALUE
    [[ -z "$CONNECT_TIMEOUT_VALUE" ]] && read -r -p "Connect timeout seconds (blank keeps current): " CONNECT_TIMEOUT_VALUE
    [[ -z "$ALLOW_OPEN_NETWORKS_VALUE" ]] && read -r -p "Allow open networks [true/false] (blank keeps current): " ALLOW_OPEN_NETWORKS_VALUE
fi

python3 - "$SCRIPT_DIR" "$CONFIG_PATH" "$IMPORT_PATH" "$IMPORT_MODE" "$MODE_VALUE" "$INTERFACE_VALUE" "$SCAN_INTERVAL_VALUE" "$SIGNAL_THRESHOLD_VALUE" "$COOLDOWN_VALUE" "$CONNECT_TIMEOUT_VALUE" "$ALLOW_OPEN_NETWORKS_VALUE" <<'PY'
import json
import os
import sys
from pathlib import Path

script_dir = Path(sys.argv[1])
config_path = Path(sys.argv[2])
import_path = sys.argv[3]
import_mode = sys.argv[4]
mode_value = sys.argv[5].strip()
interface_value = sys.argv[6]
scan_interval = sys.argv[7].strip()
signal_threshold = sys.argv[8].strip()
cooldown = sys.argv[9].strip()
connect_timeout = sys.argv[10].strip()
allow_open = sys.argv[11].strip().lower()

sys.path.insert(0, str(script_dir))
from smart_wifi_manager import load_config

with config_path.open(encoding="utf-8") as handle:
    config = json.load(handle)

if import_path:
    with open(import_path, encoding="utf-8") as handle:
        incoming = json.load(handle)
    if import_mode == "replace":
        config = incoming
    elif import_mode == "merge":
        profiles_by_id = {profile.get("id") or profile.get("ssid"): profile for profile in config.get("profiles", [])}
        for key, value in incoming.items():
            if key == "profiles":
                continue
            config[key] = value
        for profile in incoming.get("profiles", []):
            profile_id = profile.get("id") or profile.get("ssid")
            if profile_id:
                profiles_by_id[profile_id] = profile
        config["profiles"] = list(profiles_by_id.values())
    else:
        raise SystemExit(f"Unsupported import mode: {import_mode}")

if mode_value:
    config["mode"] = mode_value
if interface_value != "":
    config["interface"] = interface_value
if scan_interval:
    config["scan_interval_sec"] = int(scan_interval)
if signal_threshold:
    config["signal_switch_threshold"] = int(signal_threshold)
if cooldown:
    config["cooldown_sec"] = int(cooldown)
if connect_timeout:
    config["connect_timeout_sec"] = int(connect_timeout)
if allow_open in {"true", "false"}:
    config["allow_open_networks"] = allow_open == "true"

tmp_path = config_path.with_suffix(".tmp")
tmp_path.write_text(json.dumps(config, indent=2, sort_keys=True) + "\n", encoding="utf-8")
load_config(tmp_path)
os.replace(tmp_path, config_path)
PY

log_info "Updated $CONFIG_PATH"
log_info "Restarting smart-wifi-manager.service"
systemctl restart smart-wifi-manager.service
