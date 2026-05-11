"""Fleet profile-control helpers for Smart Wi-Fi Manager.

The helpers in this module are local-first and do not contact a fleet system.
They provide the stable profile summary, diff, dry-run, apply, and promote
semantics that MDS and other orchestrators can call through CLI or API.
"""

from __future__ import annotations

import copy
import hashlib
import json
import os
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


SCHEMA = "mds.sidecar_profile.v1"
BACKEND = "smart-wifi-manager"
PROFILE_KIND = "smart-wifi-manager-profile"
HASH_SEMANTICS = "sha256:canonical-sanitized-payload:12"

POLICY_MODES = {"observe", "local", "fleet-merge", "fleet-strict"}
DRIFT_STATES = {
    "in_sync",
    "local_extra",
    "missing_fleet_baseline",
    "outdated",
    "unmanaged",
    "unreachable",
}
SECRET_FIELD_NAMES = {"password", "passphrase", "psk", "secret", "token", "api_key", "private_key"}
EXTERNAL_SECRET_FIELD_NAMES = {
    "password_file",
    "passphrase_file",
    "secret_file",
    "token_file",
    "api_key_file",
    "private_key_file",
}


def utc_now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()


def normalize_policy_mode(value: str | None) -> str:
    mode = (value or "fleet-merge").strip().lower()
    if mode not in POLICY_MODES:
        raise ValueError("mode must be observe, local, fleet-merge, or fleet-strict")
    return mode


def _normalized_profile_id(profile: dict[str, Any]) -> str:
    return str(profile.get("id") or profile.get("ssid") or "").strip()


def secret_status(profile: dict[str, Any]) -> str:
    if str(profile.get("password_file") or "").strip():
        return "external file"
    if profile.get("has_inline_password") or str(profile.get("password") or ""):
        return "stored"
    return "missing"


def dominant_secret_status(profiles: list[dict[str, Any]]) -> str:
    states = [secret_status(profile) for profile in profiles]
    for candidate in ("external file", "stored", "redacted"):
        if candidate in states:
            return candidate
    return "missing"


def sanitized_profile(profile: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": _normalized_profile_id(profile),
        "ssid": str(profile.get("ssid") or "").strip(),
        "priority": int(profile.get("priority") or 0),
        "connection_name": str(profile.get("connection_name") or "").strip(),
        "autoconnect": bool(profile.get("autoconnect", True)),
        "disabled": bool(profile.get("disabled", False)),
        "notes": str(profile.get("notes") or "").strip(),
        "secret_status": secret_status(profile),
    }


def redacted_config(config: dict[str, Any]) -> dict[str, Any]:
    payload = copy.deepcopy(config)
    _redact_secret_fields(payload)
    return payload


def _redact_secret_fields(value: Any) -> str:
    states: list[str] = []
    if isinstance(value, dict):
        for key, raw_value in list(value.items()):
            normalized = key.lower().replace("-", "_")
            if normalized in EXTERNAL_SECRET_FIELD_NAMES:
                state = "external file" if str(raw_value or "").strip() else "missing"
                value[key] = ""
                states.append(state)
            elif normalized in SECRET_FIELD_NAMES:
                state = "stored" if str(raw_value or "").strip() else "missing"
                value[key] = ""
                states.append(state)
                if normalized == "password":
                    value["has_inline_password"] = state == "stored"
            else:
                states.append(_redact_secret_fields(raw_value))
        value["secret_status"] = _dominant_secret_status([state for state in states if state])
        if "clear_inline_password" in value:
            value["clear_inline_password"] = False
        return value["secret_status"]
    if isinstance(value, list):
        for item in value:
            states.append(_redact_secret_fields(item))
        return _dominant_secret_status([state for state in states if state])
    return "missing"


def _dominant_secret_status(states: list[str]) -> str:
    for candidate in ("external file", "stored", "redacted"):
        if candidate in states:
            return candidate
    return "missing"


def canonical_payload(config: dict[str, Any]) -> dict[str, Any]:
    profiles = [sanitized_profile(profile) for profile in config.get("profiles", []) or []]
    profiles.sort(key=lambda item: (item["id"].lower(), item["ssid"].lower()))
    return {
        "version": int(config.get("version") or 1),
        "mode": str(config.get("mode") or "manage"),
        "interface": str(config.get("interface") or ""),
        "scan_interval_sec": int(config.get("scan_interval_sec") or 0),
        "signal_switch_threshold": int(config.get("signal_switch_threshold") or 0),
        "connect_timeout_sec": int(config.get("connect_timeout_sec") or 0),
        "cooldown_sec": int(config.get("cooldown_sec") or 0),
        "allow_open_networks": bool(config.get("allow_open_networks", False)),
        "profiles": profiles,
    }


def sanitized_hash(config: dict[str, Any]) -> str:
    payload = json.dumps(canonical_payload(config), sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()[:12]


def profile_summary(
    config: dict[str, Any],
    *,
    source: str = "node-local",
    path: str | None = None,
    status: dict[str, Any] | None = None,
) -> dict[str, Any]:
    profiles = list(config.get("profiles", []) or [])
    status_profiles = list((status or {}).get("profiles", []) or [])
    return {
        "schema": SCHEMA,
        "backend": BACKEND,
        "kind": PROFILE_KIND,
        "source": source,
        "path": path,
        "present": True,
        "hash": sanitized_hash(config),
        "hash_semantics": HASH_SEMANTICS,
        "profile_count": len(profiles),
        "secret_status": dominant_secret_status(profiles),
        "profiles": [sanitized_profile(profile) for profile in profiles],
        "runtime": {
            "service_mode": config.get("mode", "manage"),
            "current_connection": (status or {}).get("current_connection", {}),
            "available_network_count": (status or {}).get("scan", {}).get("available_count"),
            "status_profile_count": len(status_profiles),
            "warnings": (status or {}).get("warnings", []),
        },
    }


def _profiles_by_id(config: dict[str, Any]) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for profile in config.get("profiles", []) or []:
        profile_id = _normalized_profile_id(profile)
        if profile_id:
            result[profile_id] = profile
    return result


def profile_diff(
    local_config: dict[str, Any] | None,
    baseline_config: dict[str, Any] | None,
    *,
    mode: str = "fleet-merge",
) -> dict[str, Any]:
    mode = normalize_policy_mode(mode)
    local_config = local_config or {}
    baseline_config = baseline_config or {}
    local_profiles = _profiles_by_id(local_config)
    baseline_profiles = _profiles_by_id(baseline_config)

    added = sorted(profile_id for profile_id in baseline_profiles if profile_id not in local_profiles)
    removed = sorted(profile_id for profile_id in local_profiles if profile_id not in baseline_profiles)
    changed = sorted(
        profile_id
        for profile_id in baseline_profiles.keys() & local_profiles.keys()
        if sanitized_profile(baseline_profiles[profile_id]) != sanitized_profile(local_profiles[profile_id])
    )

    if mode in {"observe", "local"}:
        drift_state = "unmanaged"
    elif not baseline_profiles:
        drift_state = "missing_fleet_baseline" if local_profiles else "unmanaged"
    elif not added and not changed and not removed:
        drift_state = "in_sync"
    elif mode == "fleet-merge" and removed and not added and not changed:
        drift_state = "local_extra"
    else:
        drift_state = "outdated"

    return {
        "schema": SCHEMA,
        "backend": BACKEND,
        "mode": mode,
        "drift_state": drift_state,
        "local_hash": sanitized_hash(local_config) if local_config else None,
        "baseline_hash": sanitized_hash(baseline_config) if baseline_config else None,
        "changes": {
            "add_from_baseline": added,
            "update_from_baseline": changed,
            "local_extra": removed,
            "strict_prune": removed if mode == "fleet-strict" else [],
            "preserve_local": removed if mode == "fleet-merge" else [],
        },
        "warnings": _diff_warnings(mode, removed),
    }


def _diff_warnings(mode: str, local_extra: list[str]) -> list[str]:
    warnings: list[str] = []
    if mode == "observe":
        warnings.append("observe mode reports only and will not apply profile changes")
    if mode == "local":
        warnings.append("local mode keeps the node-local profile authoritative")
    if mode == "fleet-merge" and local_extra:
        warnings.append("fleet-merge preserves local extra profiles and reports drift")
    if mode == "fleet-strict" and local_extra:
        warnings.append("fleet-strict can prune only Smart-Wi-Fi-managed NetworkManager profiles after advanced confirmation")
    return warnings


def merge_for_mode(local_config: dict[str, Any], baseline_config: dict[str, Any], *, mode: str) -> dict[str, Any]:
    mode = normalize_policy_mode(mode)
    if mode in {"observe", "local"}:
        return copy.deepcopy(local_config)
    if mode == "fleet-strict":
        return copy.deepcopy(baseline_config)

    merged = copy.deepcopy(local_config)
    for key, value in baseline_config.items():
        if key != "profiles":
            merged[key] = copy.deepcopy(value)

    local_by_id = _profiles_by_id(local_config)
    baseline_by_id = _profiles_by_id(baseline_config)
    merged_by_id = copy.deepcopy(local_by_id)
    for profile_id, baseline_profile in baseline_by_id.items():
        incoming = copy.deepcopy(baseline_profile)
        existing = local_by_id.get(profile_id)
        if existing and not incoming.get("password") and not incoming.get("password_file"):
            incoming["password"] = existing.get("password", "")
            incoming["password_file"] = existing.get("password_file", "")
        merged_by_id[profile_id] = incoming
    merged["profiles"] = sorted(
        merged_by_id.values(),
        key=lambda item: (-int(item.get("priority") or 0), str(item.get("ssid") or "").lower()),
    )
    return merged


def dry_run_plan(
    local_config: dict[str, Any],
    baseline_config: dict[str, Any],
    *,
    mode: str,
    include_candidate: bool = False,
) -> dict[str, Any]:
    mode = normalize_policy_mode(mode)
    candidate = merge_for_mode(local_config, baseline_config, mode=mode)
    diff = profile_diff(local_config, baseline_config, mode=mode)
    seed = json.dumps(
        {
            "mode": mode,
            "local": sanitized_hash(local_config),
            "baseline": sanitized_hash(baseline_config),
            "candidate": sanitized_hash(candidate),
            "created_at": int(time.time()),
        },
        sort_keys=True,
    )
    token = hashlib.sha256(seed.encode("utf-8")).hexdigest()[:16]
    plan = {
        "schema": SCHEMA,
        "backend": BACKEND,
        "kind": "smart-wifi-manager-profile-plan",
        "dry_run_id": f"swm-{token[:12]}",
        "created_at": utc_now(),
        "mode": mode,
        "confirmation_token": token,
        "requires_confirmation": mode not in {"observe", "local"},
        "requires_advanced_confirmation": mode == "fleet-strict",
        "diff": diff,
        "candidate_hash": sanitized_hash(candidate),
        "candidate_summary": profile_summary(candidate, source="candidate"),
    }
    if include_candidate:
        plan["candidate_config"] = candidate
    return plan


def redacted_plan(plan: dict[str, Any]) -> dict[str, Any]:
    payload = copy.deepcopy(plan)
    if "candidate_config" in payload:
        payload["candidate_config"] = redacted_config(payload["candidate_config"])
    return payload


def save_plan(path: Path, plan: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    data = json.dumps(plan, indent=2, sort_keys=True) + "\n"
    tmp_path = path.with_suffix(path.suffix + ".tmp")
    tmp_path.write_text(data, encoding="utf-8")
    os.chmod(tmp_path, 0o600)
    os.replace(tmp_path, path)


def apply_plan_file(plan_path: Path, config_path: Path, *, confirm: str) -> dict[str, Any]:
    plan = json.loads(plan_path.read_text(encoding="utf-8"))
    expected = str(plan.get("confirmation_token") or "")
    if not expected or confirm != expected:
        raise ValueError("confirmation token does not match dry-run plan")
    if plan.get("mode") in {"observe", "local"}:
        raise ValueError(f"{plan.get('mode')} mode does not produce apply mutations")
    candidate = plan.get("candidate_config")
    if not isinstance(candidate, dict):
        raise ValueError("dry-run plan does not contain candidate_config")
    save_config(config_path, candidate)
    return {
        "schema": SCHEMA,
        "backend": BACKEND,
        "applied": True,
        "mode": plan.get("mode"),
        "dry_run_id": plan.get("dry_run_id"),
        "applied_at": utc_now(),
        "candidate_hash": sanitized_hash(candidate),
    }


def save_config(path: Path, config: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp_path = path.with_suffix(path.suffix + ".tmp")
    tmp_path.write_text(json.dumps(config, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    os.chmod(tmp_path, 0o600)
    os.replace(tmp_path, path)


def validate_bundle(config: dict[str, Any]) -> dict[str, Any]:
    errors: list[str] = []
    seen: set[str] = set()
    profiles = config.get("profiles", [])
    if not isinstance(profiles, list):
        errors.append("profiles must be a list")
        profiles = []
    for index, profile in enumerate(profiles):
        if not isinstance(profile, dict):
            errors.append(f"profiles[{index}] must be an object")
            continue
        profile_id = _normalized_profile_id(profile)
        if not profile_id:
            errors.append(f"profiles[{index}] is missing id or ssid")
        if profile_id in seen:
            errors.append(f"duplicate profile id: {profile_id}")
        seen.add(profile_id)
        if not str(profile.get("ssid") or "").strip():
            errors.append(f"profiles[{index}] is missing ssid")
        try:
            priority = int(profile.get("priority") or 0)
            if priority < 0:
                errors.append(f"profiles[{index}] priority must be non-negative")
        except (TypeError, ValueError):
            errors.append(f"profiles[{index}] priority must be an integer")
    return {
        "schema": SCHEMA,
        "backend": BACKEND,
        "valid": not errors,
        "errors": errors,
        "profile_count": len(profiles),
        "hash": sanitized_hash(config) if not errors else None,
        "hash_semantics": HASH_SEMANTICS,
    }


def promote_reference_draft(config: dict[str, Any], *, redacted: bool = True) -> dict[str, Any]:
    draft = copy.deepcopy(config)
    draft.setdefault("version", 1)
    draft["profiles"] = list(draft.get("profiles", []) or [])
    return {
        "schema": SCHEMA,
        "backend": BACKEND,
        "kind": PROFILE_KIND,
        "created_at": utc_now(),
        "profile": redacted_config(draft) if redacted else draft,
        "summary": profile_summary(draft, source="reference-draft"),
    }


def write_audit_entry(state_dir: Path, action: str, payload: dict[str, Any]) -> None:
    audit_dir = state_dir / "audit"
    audit_dir.mkdir(parents=True, exist_ok=True)
    entry = {
        "timestamp": utc_now(),
        "action": action,
        "backend": BACKEND,
        "payload": payload,
    }
    with (audit_dir / "profile-control.jsonl").open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(entry, sort_keys=True) + "\n")
