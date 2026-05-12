from __future__ import annotations

import json
import logging

import smart_wifi_manager
from smart_wifi_manager import choose_target_profile, connect_profile, load_config, main, redacted_config
from profile_control import dry_run_plan, profile_diff, profile_summary, validate_bundle


def test_load_config_normalizes_and_sorts_profiles(tmp_path):
    config_path = tmp_path / "config.json"
    config_path.write_text(
        json.dumps(
            {
                "mode": "manage",
                "profiles": [
                    {"id": "b", "ssid": "B", "priority": 20},
                    {"id": "a", "ssid": "A", "priority": 80},
                ],
            }
        ),
        encoding="utf-8",
    )

    config = load_config(config_path)

    assert config["mode"] == "manage"
    assert [profile["id"] for profile in config["profiles"]] == ["a", "b"]


def test_redacted_config_hides_inline_passwords(tmp_path):
    config_path = tmp_path / "config.json"
    config_path.write_text(
        json.dumps(
            {
                "profiles": [
                    {"id": "home", "ssid": "Home", "priority": 100, "password": "secret"},
                ]
            }
        ),
        encoding="utf-8",
    )

    redacted = redacted_config(load_config(config_path))

    assert redacted["profiles"][0]["password"] == ""
    assert redacted["profiles"][0]["has_inline_password"] is True


def test_command_logging_redacts_password_arguments():
    command = [
        "nmcli",
        "dev",
        "wifi",
        "connect",
        "FieldNet",
        "password",
        "secret-value",
        "802-11-wireless-security.psk",
        "another-secret",
    ]

    redacted = smart_wifi_manager.redact_command_args(command)

    assert "secret-value" not in redacted
    assert "another-secret" not in redacted
    assert redacted[6] == "REDACTED"
    assert redacted[8] == "REDACTED"


def test_choose_target_profile_prefers_higher_priority_then_signal():
    config = {
        "signal_switch_threshold": 20,
        "allow_open_networks": False,
        "profiles": [
            {"id": "home", "ssid": "Home", "priority": 100, "password": "secret", "password_file": "", "disabled": False},
            {"id": "backup", "ssid": "Backup", "priority": 50, "password": "secret", "password_file": "", "disabled": False},
        ],
    }
    current = {"connected": True, "ssid": "Backup", "signal": 90}
    visible = [
        {"ssid": "Home", "signal": 40, "security": "WPA2", "in_use": False},
        {"ssid": "Backup", "signal": 90, "security": "WPA2", "in_use": True},
    ]

    selection = choose_target_profile(config, current, visible)

    assert selection.profile is not None
    assert selection.profile["ssid"] == "Home"
    assert selection.reason == "better-network-available"


def test_choose_target_profile_respects_signal_threshold_for_same_priority():
    config = {
        "signal_switch_threshold": 20,
        "allow_open_networks": False,
        "profiles": [
            {"id": "home-a", "ssid": "Home-A", "priority": 100, "password": "secret", "password_file": "", "disabled": False},
            {"id": "home-b", "ssid": "Home-B", "priority": 100, "password": "secret", "password_file": "", "disabled": False},
        ],
    }
    current = {"connected": True, "ssid": "Home-A", "signal": 50}
    visible = [
        {"ssid": "Home-A", "signal": 50, "security": "WPA2", "in_use": True},
        {"ssid": "Home-B", "signal": 60, "security": "WPA2", "in_use": False},
    ]

    selection = choose_target_profile(config, current, visible)

    assert selection.profile is None
    assert selection.reason == "signal-gain-below-threshold"


def test_connect_profile_repairs_secured_networkmanager_connection(monkeypatch):
    calls = []

    def fake_run_command(command, logger, timeout=0):
        calls.append(command)
        if command[3:5] == ["connection", "modify"]:
            return 0, "modified", ""
        if command[3:5] == ["connection", "up"]:
            return 0, "activated", ""
        if command[3:5] == ["dev", "wifi"]:
            return 0, "connected", ""
        return 1, "", "unexpected"

    monkeypatch.setattr(smart_wifi_manager, "run_command", fake_run_command)

    ok, message = connect_profile(
        "wlan0",
        {"id": "field", "ssid": "FieldNet", "priority": 100, "password": "secret", "password_file": "", "connection_name": ""},
        {"connect_timeout_sec": 10},
        logging.getLogger("test"),
    )

    assert ok is True
    assert message == "activated"
    assert calls[0] == [
        "timeout",
        "10",
        "nmcli",
        "connection",
        "modify",
        "FieldNet",
        "802-11-wireless.ssid",
        "FieldNet",
        "802-11-wireless-security.key-mgmt",
        "wpa-psk",
        "802-11-wireless-security.psk",
        "secret",
        "connection.autoconnect",
        "yes",
    ]
    assert calls[1] == [
        "timeout",
        "10",
        "nmcli",
        "connection",
        "up",
        "id",
        "FieldNet",
        "ifname",
        "wlan0",
    ]
    assert calls[2] == [
        "timeout",
        "12",
        "nmcli",
        "connection",
        "modify",
        "FieldNet",
        "connection.user-data",
        "smart-wifi-manager.managed=true,smart-wifi-manager.profile-id=field",
    ]
    assert len(calls) == 3


def test_connect_profile_creates_named_connection_after_repair_misses(monkeypatch):
    calls = []

    def fake_run_command(command, logger, timeout=0):
        calls.append(command)
        if command[3:5] == ["connection", "modify"]:
            return 10, "", "unknown connection"
        if command[3:5] == ["connection", "up"]:
            return 10, "", "unknown connection"
        if command[3:5] == ["dev", "wifi"]:
            return 0, "connected", ""
        return 1, "", "unexpected"

    monkeypatch.setattr(smart_wifi_manager, "run_command", fake_run_command)

    ok, message = connect_profile(
        "wlan0",
        {"id": "field", "ssid": "FieldNet", "priority": 100, "password": "secret", "password_file": "", "connection_name": ""},
        {"connect_timeout_sec": 10},
        logging.getLogger("test"),
    )

    assert ok is True
    assert message == "connected"
    assert calls[-2] == [
        "timeout",
        "10",
        "nmcli",
        "dev",
        "wifi",
        "connect",
        "FieldNet",
        "ifname",
        "wlan0",
        "password",
        "secret",
        "name",
        "FieldNet",
    ]
    assert calls[-1] == [
        "timeout",
        "12",
        "nmcli",
        "connection",
        "modify",
        "FieldNet",
        "connection.user-data",
        "smart-wifi-manager.managed=true,smart-wifi-manager.profile-id=field",
    ]
    assert len(calls) == 4


def test_profile_summary_hash_redacts_inline_passwords():
    local = {
        "version": 1,
        "mode": "manage",
        "profiles": [{"id": "field", "ssid": "DemoField", "priority": 100, "password": "one"}],
    }
    other = {
        "version": 1,
        "mode": "manage",
        "profiles": [{"id": "field", "ssid": "DemoField", "priority": 100, "password": "two"}],
    }

    assert profile_summary(local)["hash"] == profile_summary(other)["hash"]
    assert profile_summary(local)["secret_status"] == "stored"


def test_profile_diff_fleet_merge_preserves_local_extra():
    local = {
        "profiles": [
            {"id": "fleet", "ssid": "Fleet", "priority": 100},
            {"id": "local", "ssid": "LocalEmergency", "priority": 10},
        ]
    }
    baseline = {"profiles": [{"id": "fleet", "ssid": "Fleet", "priority": 100}]}

    diff = profile_diff(local, baseline, mode="fleet-merge")

    assert diff["drift_state"] == "local_extra"
    assert diff["changes"]["preserve_local"] == ["local"]
    assert diff["changes"]["strict_prune"] == []


def test_profile_diff_fleet_merge_outdated_when_baseline_updates_and_local_extra():
    local = {
        "profiles": [
            {"id": "fleet", "ssid": "Fleet", "priority": 10},
            {"id": "local", "ssid": "LocalEmergency", "priority": 10},
        ]
    }
    baseline = {"profiles": [{"id": "fleet", "ssid": "Fleet", "priority": 100}]}

    diff = profile_diff(local, baseline, mode="fleet-merge")

    assert diff["drift_state"] == "outdated"
    assert diff["changes"]["preserve_local"] == ["local"]


def test_profile_diff_fleet_strict_marks_prune_candidates():
    local = {
        "profiles": [
            {"id": "fleet", "ssid": "Fleet", "priority": 100},
            {"id": "local", "ssid": "LocalEmergency", "priority": 10},
        ]
    }
    baseline = {"profiles": [{"id": "fleet", "ssid": "Fleet", "priority": 100}]}

    diff = profile_diff(local, baseline, mode="fleet-strict")

    assert diff["drift_state"] == "outdated"
    assert diff["changes"]["strict_prune"] == ["local"]
    assert "advanced confirmation" in diff["warnings"][0]


def test_validate_bundle_reports_duplicate_ids():
    result = validate_bundle(
        {
            "profiles": [
                {"id": "field", "ssid": "Field", "priority": 100},
                {"id": "field", "ssid": "Field2", "priority": 90},
            ]
        }
    )

    assert result["valid"] is False
    assert "duplicate profile id: field" in result["errors"]


def test_dry_run_plan_requires_confirmation_and_holds_candidate():
    local = {
        "profiles": [
            {"id": "fleet", "ssid": "Fleet", "priority": 100},
            {"id": "local", "ssid": "Local", "priority": 10},
        ]
    }
    baseline = {"profiles": [{"id": "fleet", "ssid": "Fleet", "priority": 100}]}

    plan = dry_run_plan(local, baseline, mode="fleet-merge", include_candidate=True)

    assert plan["requires_confirmation"] is True
    assert plan["diff"]["drift_state"] == "local_extra"
    assert {profile["id"] for profile in plan["candidate_config"]["profiles"]} == {"fleet", "local"}


def test_profile_cli_import_apply_roundtrip(tmp_path, capsys):
    config_path = tmp_path / "config.json"
    baseline_path = tmp_path / "baseline.json"
    plan_path = tmp_path / "plan.json"
    state_dir = tmp_path / "state"
    config_path.write_text(
        json.dumps({"version": 1, "mode": "manage", "profiles": [{"id": "local", "ssid": "Local", "priority": 10}]}),
        encoding="utf-8",
    )
    baseline_path.write_text(
        json.dumps({"version": 1, "mode": "manage", "profiles": [{"id": "fleet", "ssid": "Fleet", "priority": 100}]}),
        encoding="utf-8",
    )

    assert main([
        "profile",
        "import",
        "--config",
        str(config_path),
        "--file",
        str(baseline_path),
        "--mode",
        "fleet-merge",
        "--dry-run",
        "--output-plan",
        str(plan_path),
        "--state-dir",
        str(state_dir),
    ]) == 0
    out = json.loads(capsys.readouterr().out)
    token = out["confirmation_token"]
    assert out["diff"]["changes"]["preserve_local"] == ["local"]
    assert plan_path.exists()

    assert main([
        "profile",
        "apply",
        "--config",
        str(config_path),
        "--plan",
        str(plan_path),
        "--confirm",
        token,
        "--state-dir",
        str(state_dir),
    ]) == 0
    applied = json.loads(capsys.readouterr().out)
    updated = load_config(config_path)
    assert applied["applied"] is True
    assert {profile["id"] for profile in updated["profiles"]} == {"fleet", "local"}
    assert (state_dir / "audit" / "profile-control.jsonl").exists()


def test_profile_cli_export_redacts_by_default(tmp_path, capsys):
    config_path = tmp_path / "config.json"
    config_path.write_text(
        json.dumps({"version": 1, "profiles": [{"id": "field", "ssid": "Field", "password": "secret-value"}]}),
        encoding="utf-8",
    )

    assert main(["profile", "export", "--config", str(config_path)]) == 0
    exported = json.loads(capsys.readouterr().out)

    assert exported["profiles"][0]["password"] == ""
    assert exported["profiles"][0]["secret_status"] == "stored"
    assert "secret-value" not in json.dumps(exported)


def test_profile_cli_export_redacts_extra_secret_fields(tmp_path, capsys):
    config_path = tmp_path / "config.json"
    config_path.write_text(
        json.dumps(
            {
                "version": 1,
                "profiles": [
                    {"id": "field", "ssid": "Field", "password_file": "/run/private/pass", "metadata": {"token": "abc"}}
                ],
            }
        ),
        encoding="utf-8",
    )

    assert main(["profile", "export", "--config", str(config_path)]) == 0
    exported_text = capsys.readouterr().out
    exported = json.loads(exported_text)

    assert exported["profiles"][0]["password_file"] == ""
    assert exported["profiles"][0]["secret_status"] == "external file"
    assert "abc" not in exported_text
    assert "/run/private/pass" not in exported_text
