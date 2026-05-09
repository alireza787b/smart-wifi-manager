from __future__ import annotations

import json
import logging

import smart_wifi_manager
from smart_wifi_manager import choose_target_profile, connect_profile, load_config, redacted_config


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
    assert len(calls) == 2


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
    assert calls[-1] == [
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
    assert len(calls) == 3
