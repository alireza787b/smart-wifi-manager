#!/usr/bin/env python3
"""Smart Wi-Fi Manager core service.

This service keeps a Linux host connected to the best available known Wi-Fi
profile using NetworkManager (`nmcli`). It owns the canonical config and status
files used by the optional web dashboard.
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import platform
import shlex
import socket
import subprocess
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from logging.handlers import RotatingFileHandler
from pathlib import Path
from typing import Any


def load_version() -> str:
    version_file = Path(__file__).with_name("VERSION")
    if version_file.exists():
        return version_file.read_text(encoding="utf-8").strip()
    return "2.0.0"


VERSION = load_version()
DEFAULT_CONFIG_PATH = Path("/etc/smart-wifi-manager/config.json")
DEFAULT_STATUS_PATH = Path("/run/smart-wifi-manager/status.json")
DEFAULT_STATE_DIR = Path("/var/lib/smart-wifi-manager")
DEFAULT_LOG_PATH = Path("/var/log/smart-wifi-manager/smart-wifi-manager.log")
DEFAULT_CONTROL_DIR_NAME = "control"
DEFAULT_SCAN_TRIGGER_FILE = "scan-now"
DEFAULT_RELOAD_TRIGGER_FILE = "reload"


def utc_now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()


def atomic_write_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp_path = path.with_suffix(path.suffix + ".tmp")
    tmp_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    os.replace(tmp_path, path)


def normalize_mode(value: str | None) -> str:
    normalized = (value or "manage").strip().lower()
    if normalized in {"manage", "observe", "disabled"}:
        return normalized
    raise ValueError(f"Unsupported mode: {value!r}")


def normalize_interface(value: str | None) -> str:
    return (value or "").strip()


def normalize_int(value: Any, default: int, minimum: int = 0) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return default
    return max(parsed, minimum)


def sanitize_profile(raw: dict[str, Any], index: int) -> dict[str, Any]:
    ssid = str(raw.get("ssid", "")).strip()
    if not ssid:
        raise ValueError(f"profiles[{index}] is missing ssid")

    priority = normalize_int(raw.get("priority", 100), 100, minimum=0)
    profile_id = str(raw.get("id") or ssid).strip()
    connection_name = str(raw.get("connection_name", "")).strip()
    password = str(raw.get("password", ""))
    password_file = str(raw.get("password_file", "")).strip()
    disabled = bool(raw.get("disabled", False))

    return {
        "id": profile_id,
        "ssid": ssid,
        "priority": priority,
        "connection_name": connection_name,
        "password": password,
        "password_file": password_file,
        "autoconnect": bool(raw.get("autoconnect", True)),
        "disabled": disabled,
        "notes": str(raw.get("notes", "")).strip(),
    }


def default_config() -> dict[str, Any]:
    return {
        "version": 1,
        "mode": "manage",
        "interface": "",
        "scan_interval_sec": 10,
        "signal_switch_threshold": 20,
        "connect_timeout_sec": 10,
        "cooldown_sec": 60,
        "allow_open_networks": False,
        "profiles": [],
    }


def load_config(path: Path) -> dict[str, Any]:
    if not path.exists():
        raise FileNotFoundError(f"Configuration file not found: {path}")

    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"Configuration file {path} must contain a JSON object")

    config = default_config()
    config.update(payload)
    config["mode"] = normalize_mode(config.get("mode"))
    config["interface"] = normalize_interface(config.get("interface"))
    config["scan_interval_sec"] = normalize_int(config.get("scan_interval_sec"), 10, minimum=2)
    config["signal_switch_threshold"] = normalize_int(config.get("signal_switch_threshold"), 20, minimum=0)
    config["connect_timeout_sec"] = normalize_int(config.get("connect_timeout_sec"), 10, minimum=3)
    config["cooldown_sec"] = normalize_int(config.get("cooldown_sec"), 60, minimum=0)
    config["allow_open_networks"] = bool(config.get("allow_open_networks", False))

    raw_profiles = config.get("profiles", [])
    if not isinstance(raw_profiles, list):
        raise ValueError("profiles must be a list")

    seen_ids: set[str] = set()
    sanitized_profiles = []
    for index, raw_profile in enumerate(raw_profiles):
        if not isinstance(raw_profile, dict):
            raise ValueError(f"profiles[{index}] must be an object")
        profile = sanitize_profile(raw_profile, index)
        if profile["id"] in seen_ids:
            raise ValueError(f"Duplicate profile id: {profile['id']}")
        seen_ids.add(profile["id"])
        sanitized_profiles.append(profile)

    sanitized_profiles.sort(key=lambda item: (-item["priority"], item["ssid"].lower()))
    config["profiles"] = sanitized_profiles
    return config


def redacted_config(config: dict[str, Any]) -> dict[str, Any]:
    payload = json.loads(json.dumps(config))
    for profile in payload.get("profiles", []):
        inline_password = bool(profile.get("password"))
        profile["has_inline_password"] = inline_password
        if inline_password:
            profile["password"] = ""
    return payload


def run_command(command: list[str], logger: logging.Logger, timeout: int = 15) -> tuple[int, str, str]:
    logger.debug("Executing command: %s", " ".join(shlex.quote(part) for part in command))
    proc = subprocess.run(
        command,
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
    )
    return proc.returncode, proc.stdout.strip(), proc.stderr.strip()


def nmcli_present() -> bool:
    return shutil_which("nmcli") is not None


def systemctl_present() -> bool:
    return shutil_which("systemctl") is not None


def shutil_which(binary: str) -> str | None:
    for directory in os.environ.get("PATH", "").split(os.pathsep):
        if not directory:
            continue
        candidate = Path(directory) / binary
        if candidate.is_file() and os.access(candidate, os.X_OK):
            return str(candidate)
    return None


def network_manager_active(logger: logging.Logger) -> bool | None:
    if not systemctl_present():
        return None
    code, _, _ = run_command(["systemctl", "is-active", "NetworkManager"], logger, timeout=5)
    if code == 0:
        return True
    if code in {1, 3}:
        return False
    return None


def detect_wifi_interface(requested: str, logger: logging.Logger) -> tuple[str | None, list[str]]:
    warnings: list[str] = []
    if not nmcli_present():
        warnings.append("nmcli is not installed")
        return None, warnings

    code, stdout, stderr = run_command(
        ["nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device", "status"],
        logger,
        timeout=10,
    )
    if code != 0:
        warnings.append(f"Failed to query NetworkManager devices: {stderr or stdout or 'unknown error'}")
        return None, warnings

    wifi_devices: list[str] = []
    for line in stdout.splitlines():
        parts = line.split(":", 3)
        if len(parts) < 2:
            continue
        device, dev_type = parts[0], parts[1]
        if dev_type == "wifi":
            wifi_devices.append(device)

    if requested:
        if requested in wifi_devices:
            return requested, warnings
        warnings.append(f"Requested Wi-Fi interface {requested!r} not found")
        return None, warnings

    if not wifi_devices:
        warnings.append("No Wi-Fi interface detected by NetworkManager")
        return None, warnings

    return wifi_devices[0], warnings


def current_connection(interface: str | None, logger: logging.Logger) -> dict[str, Any]:
    payload = {
        "interface": interface or "",
        "ssid": "",
        "connection_name": "",
        "signal": 0,
        "connected": False,
    }
    if not interface or not nmcli_present():
        return payload

    code, stdout, _ = run_command(
        ["nmcli", "-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "dev", "wifi", "list", "ifname", interface],
        logger,
        timeout=10,
    )
    if code != 0:
        return payload

    for line in stdout.splitlines():
        parts = line.split(":", 3)
        if len(parts) < 4:
            continue
        in_use, ssid, signal, _security = parts
        if in_use == "*":
            payload["ssid"] = ssid
            payload["signal"] = normalize_int(signal, 0, minimum=0)
            payload["connected"] = True
            break

    code, stdout, _ = run_command(
        ["nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device", "status"],
        logger,
        timeout=10,
    )
    if code == 0:
        for line in stdout.splitlines():
            parts = line.split(":", 3)
            if len(parts) < 4:
                continue
            device, dev_type, state, connection_name = parts
            if device == interface and dev_type == "wifi" and state == "connected":
                payload["connection_name"] = connection_name
                payload["connected"] = True
                break

    return payload


def scan_networks(interface: str | None, logger: logging.Logger) -> tuple[list[dict[str, Any]], list[str]]:
    warnings: list[str] = []
    if not interface:
        warnings.append("No Wi-Fi interface available for scanning")
        return [], warnings
    if not nmcli_present():
        warnings.append("nmcli is not installed")
        return [], warnings

    code, stdout, stderr = run_command(
        ["nmcli", "-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "dev", "wifi", "list", "ifname", interface, "--rescan", "yes"],
        logger,
        timeout=20,
    )
    if code != 0:
        warnings.append(f"Failed to scan Wi-Fi networks: {stderr or stdout or 'unknown error'}")
        return [], warnings

    networks: list[dict[str, Any]] = []
    for line in stdout.splitlines():
        parts = line.split(":", 3)
        if len(parts) < 4:
            continue
        in_use, ssid, signal, security = parts
        ssid = ssid.strip()
        if not ssid:
            continue
        networks.append(
            {
                "ssid": ssid,
                "signal": normalize_int(signal, 0, minimum=0),
                "security": security.strip(),
                "in_use": in_use == "*",
            }
        )

    best_by_ssid: dict[str, dict[str, Any]] = {}
    for network in networks:
        current = best_by_ssid.get(network["ssid"])
        if current is None or network["signal"] > current["signal"]:
            best_by_ssid[network["ssid"]] = network

    deduped = sorted(best_by_ssid.values(), key=lambda item: (-item["signal"], item["ssid"].lower()))
    return deduped, warnings


@dataclass
class SelectionResult:
    profile: dict[str, Any] | None
    reason: str


def choose_target_profile(
    config: dict[str, Any],
    current: dict[str, Any],
    available_networks: list[dict[str, Any]],
) -> SelectionResult:
    visible = {network["ssid"]: network for network in available_networks}

    candidates: list[dict[str, Any]] = []
    for profile in config["profiles"]:
        if profile["disabled"]:
            continue
        network = visible.get(profile["ssid"])
        if network is None:
            continue
        if not config["allow_open_networks"] and not network["security"] and not profile["password"] and not profile["password_file"]:
            continue
        candidate = dict(profile)
        candidate["signal"] = network["signal"]
        candidate["security"] = network["security"]
        candidate["current"] = current.get("ssid") == profile["ssid"]
        candidates.append(candidate)

    if not candidates:
        return SelectionResult(profile=None, reason="no-known-network-visible")

    candidates.sort(key=lambda item: (-item["priority"], -item["signal"], item["ssid"].lower()))
    best = candidates[0]

    if current.get("connected") and current.get("ssid") == best["ssid"]:
        return SelectionResult(profile=None, reason="already-on-best-network")

    if current.get("connected"):
        current_candidate = next((item for item in candidates if item["ssid"] == current.get("ssid")), None)
        if current_candidate is not None:
            if best["priority"] < current_candidate["priority"]:
                return SelectionResult(profile=None, reason="current-network-higher-priority")
            if best["priority"] == current_candidate["priority"]:
                gain = best["signal"] - current_candidate["signal"]
                if gain < config["signal_switch_threshold"]:
                    return SelectionResult(profile=None, reason="signal-gain-below-threshold")

    return SelectionResult(profile=best, reason="better-network-available")


def profile_password(profile: dict[str, Any]) -> str:
    password_file = profile.get("password_file", "")
    if password_file:
        return Path(password_file).read_text(encoding="utf-8").strip()
    return profile.get("password", "")


def connect_profile(interface: str | None, profile: dict[str, Any], config: dict[str, Any], logger: logging.Logger) -> tuple[bool, str]:
    if not interface:
        return False, "No Wi-Fi interface available"

    ssid = profile["ssid"]
    connection_name = profile.get("connection_name") or ssid
    password = profile_password(profile)
    timeout = str(config["connect_timeout_sec"])

    commands: list[list[str]] = []
    if profile.get("connection_name"):
        commands.append(["timeout", timeout, "nmcli", "connection", "up", "id", connection_name, "ifname", interface])

    connect_cmd = ["timeout", timeout, "nmcli", "dev", "wifi", "connect", ssid, "ifname", interface]
    if password:
        connect_cmd.extend(["password", password])
    commands.append(connect_cmd)

    for command in commands:
        code, stdout, stderr = run_command(command, logger, timeout=config["connect_timeout_sec"] + 2)
        if code == 0:
            return True, stdout or f"Connected to {ssid}"
        logger.warning("Wi-Fi connection attempt failed for %s via %s: %s", ssid, command[2], stderr or stdout)

    return False, stderr or stdout or f"Failed to connect to {ssid}"


def configure_logger(log_path: Path, verbose: bool = False) -> logging.Logger:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    logger = logging.getLogger("smart_wifi_manager")
    logger.setLevel(logging.DEBUG if verbose else logging.INFO)
    logger.handlers.clear()

    formatter = logging.Formatter("%(asctime)s [%(levelname)s] %(message)s")

    file_handler = RotatingFileHandler(log_path, maxBytes=5 * 1024 * 1024, backupCount=3)
    file_handler.setFormatter(formatter)
    file_handler.setLevel(logging.DEBUG)
    logger.addHandler(file_handler)

    stream_handler = logging.StreamHandler(sys.stdout)
    stream_handler.setFormatter(formatter)
    stream_handler.setLevel(logging.DEBUG if verbose else logging.INFO)
    logger.addHandler(stream_handler)

    return logger


def consume_control_file(path: Path) -> bool:
    if not path.exists():
        return False
    try:
        path.unlink()
    except FileNotFoundError:
        return False
    return True


def service_loop(config_path: Path, status_path: Path, state_dir: Path, log_path: Path, once: bool, verbose: bool) -> int:
    logger = configure_logger(log_path, verbose=verbose)
    control_dir = state_dir / DEFAULT_CONTROL_DIR_NAME
    control_dir.mkdir(parents=True, exist_ok=True)

    last_switch_at = 0.0
    last_switch_reason = ""
    last_switch_target = ""
    logger.info("smart-wifi-manager %s starting", VERSION)

    while True:
        warnings: list[str] = []
        try:
            config = load_config(config_path)
        except Exception as exc:
            logger.error("Failed to load config %s: %s", config_path, exc)
            atomic_write_json(
                status_path,
                {
                    "version": VERSION,
                    "timestamp": utc_now(),
                    "error": str(exc),
                    "config_path": str(config_path),
                    "warnings": [str(exc)],
                    "service": {"mode": "error"},
                },
            )
            if once:
                return 1
            time.sleep(5)
            continue

        requested_scan = consume_control_file(control_dir / DEFAULT_SCAN_TRIGGER_FILE)
        consume_control_file(control_dir / DEFAULT_RELOAD_TRIGGER_FILE)

        interface, interface_warnings = detect_wifi_interface(config["interface"], logger)
        warnings.extend(interface_warnings)
        current = current_connection(interface, logger)

        available_networks: list[dict[str, Any]] = []
        scan_warnings: list[str] = []
        if config["mode"] in {"manage", "observe"} or requested_scan:
            available_networks, scan_warnings = scan_networks(interface, logger)
            warnings.extend(scan_warnings)

        selected = choose_target_profile(config, current, available_networks)
        switch_attempted = False
        switch_result = "not-attempted"

        if config["mode"] == "manage" and selected.profile is not None:
            now = time.time()
            if requested_scan or (now - last_switch_at) >= config["cooldown_sec"]:
                switch_attempted = True
                ok, message = connect_profile(interface, selected.profile, config, logger)
                switch_result = message
                if ok:
                    last_switch_at = now
                    last_switch_reason = selected.reason
                    last_switch_target = selected.profile["ssid"]
                    logger.info("Switched Wi-Fi to %s (%s)", selected.profile["ssid"], selected.reason)
                    current = current_connection(interface, logger)
                else:
                    warnings.append(message)
            else:
                switch_result = "cooldown-active"

        status_payload = {
            "version": VERSION,
            "timestamp": utc_now(),
            "hostname": socket.gethostname(),
            "service": {
                "mode": config["mode"],
                "config_path": str(config_path),
                "status_path": str(status_path),
                "state_dir": str(state_dir),
                "log_path": str(log_path),
                "requested_scan": requested_scan,
            },
            "system": {
                "platform": platform.platform(),
                "python": platform.python_version(),
                "nmcli_present": nmcli_present(),
                "network_manager_active": network_manager_active(logger),
            },
            "interface": {
                "requested": config["interface"],
                "active": interface or "",
            },
            "current_connection": current,
            "selection": {
                "reason": selected.reason,
                "target_ssid": selected.profile["ssid"] if selected.profile else "",
                "switch_attempted": switch_attempted,
                "switch_result": switch_result,
                "last_switch_at": datetime.fromtimestamp(last_switch_at, timezone.utc).replace(microsecond=0).isoformat()
                if last_switch_at
                else "",
                "last_switch_reason": last_switch_reason,
                "last_switch_target": last_switch_target,
            },
            "scan": {
                "available_networks": available_networks,
                "available_count": len(available_networks),
            },
            "profiles": [
                {
                    "id": profile["id"],
                    "ssid": profile["ssid"],
                    "priority": profile["priority"],
                    "connection_name": profile["connection_name"],
                    "password_file": profile["password_file"],
                    "has_inline_password": bool(profile["password"]),
                    "disabled": profile["disabled"],
                    "current": current.get("ssid") == profile["ssid"],
                    "visible": any(network["ssid"] == profile["ssid"] for network in available_networks),
                    "signal": next((network["signal"] for network in available_networks if network["ssid"] == profile["ssid"]), 0),
                }
                for profile in config["profiles"]
            ],
            "warnings": warnings,
        }
        atomic_write_json(status_path, status_payload)

        if once:
            return 0

        time.sleep(config["scan_interval_sec"])


def print_json(path: Path, redacted: bool = False) -> int:
    if redacted:
        payload = redacted_config(load_config(path))
    else:
        payload = load_config(path)
    print(json.dumps(payload, indent=2, sort_keys=True))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Smart Wi-Fi Manager")
    subparsers = parser.add_subparsers(dest="command", required=True)

    run_parser = subparsers.add_parser("run", help="Run the Wi-Fi manager loop")
    run_parser.add_argument("--config", default=str(DEFAULT_CONFIG_PATH))
    run_parser.add_argument("--status-file", default=str(DEFAULT_STATUS_PATH))
    run_parser.add_argument("--state-dir", default=str(DEFAULT_STATE_DIR))
    run_parser.add_argument("--log-file", default=str(DEFAULT_LOG_PATH))
    run_parser.add_argument("--once", action="store_true")
    run_parser.add_argument("--verbose", action="store_true")

    validate_parser = subparsers.add_parser("validate-config", help="Validate config and exit")
    validate_parser.add_argument("--config", default=str(DEFAULT_CONFIG_PATH))

    print_parser = subparsers.add_parser("print-config", help="Print config and exit")
    print_parser.add_argument("--config", default=str(DEFAULT_CONFIG_PATH))
    print_parser.add_argument("--redacted", action="store_true")

    version_parser = subparsers.add_parser("version", help="Print version")
    version_parser.set_defaults(version=True)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    if args.command == "version":
        print(f"smart-wifi-manager {VERSION}")
        return 0

    config_path = Path(getattr(args, "config", DEFAULT_CONFIG_PATH))

    if args.command == "validate-config":
        load_config(config_path)
        print(f"Config valid: {config_path}")
        return 0

    if args.command == "print-config":
        return print_json(config_path, redacted=bool(args.redacted))

    if args.command == "run":
        return service_loop(
            config_path=config_path,
            status_path=Path(args.status_file),
            state_dir=Path(args.state_dir),
            log_path=Path(args.log_file),
            once=bool(args.once),
            verbose=bool(args.verbose),
        )

    parser.print_help()
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
