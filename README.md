# Smart Wi-Fi Manager

Smart Wi-Fi Manager keeps a Linux companion computer connected to the best
available known Wi-Fi profile using NetworkManager (`nmcli`).

It is built as a standalone product:

- generic Linux companion-computer utility
- structured JSON config
- optional lightweight web dashboard on port `9080`
- install/configure scripts for operators
- file-based status and control surfaces for AI agents and automation

This repo is intentionally **not MDS-specific**. MDS can integrate it later as
an optional connectivity backend, but the tool stands on its own.

## Dashboard Preview

![Smart Wi-Fi Manager dashboard](docs/images/dashboard-overview.png)

## What It Does

- watches Wi-Fi availability through NetworkManager
- tracks the current connection and visible candidate networks
- chooses the best known profile using priority + signal policy
- switches only when policy says it should
- writes live status to a predictable JSON file
- exposes config/status/logs through a local dashboard/API

## Runtime Model

Canonical files:

- config: `/etc/smart-wifi-manager/config.json`
- status: `/run/smart-wifi-manager/status.json`
- state/control: `/var/lib/smart-wifi-manager`
- logs: `/var/log/smart-wifi-manager/smart-wifi-manager.log`

Modes:

- `manage`: scan and switch
- `observe`: scan/report only, no switching
- `disabled`: stay installed but inactive

## Quick Start

### 1. Clone

```bash
git clone https://github.com/alireza787b/smart-wifi-manager.git
cd smart-wifi-manager
```

### 2. Install

```bash
sudo ./install.sh
```

This installs:

- `smart-wifi-manager.service`
- default config at `/etc/smart-wifi-manager/config.json`
- optional dashboard service on `127.0.0.1:9080`

Skip the dashboard if you only want the core service:

```bash
sudo ./install.sh --skip-dashboard
```

Expose the dashboard on the LAN or VPN only if you actually want remote access:

```bash
sudo ./install.sh --dashboard-listen 0.0.0.0:9080
```

### 3. Configure

```bash
sudo ./configure_smart_wifi_manager.sh
```

Or headless:

```bash
sudo ./configure_smart_wifi_manager.sh --headless \
  --mode manage \
  --scan-interval 10 \
  --signal-threshold 20 \
  --cooldown 60
```

Import a prepared config bundle:

```bash
sudo ./configure_smart_wifi_manager.sh --headless \
  --import ./my-wifi-config.json \
  --import-mode replace
```

### 4. Verify

```bash
sudo systemctl status smart-wifi-manager.service
cat /run/smart-wifi-manager/status.json
```

If dashboard is installed:

```text
http://127.0.0.1:9080
```

## Config Model

The canonical config is structured JSON. Example:

```json
{
  "version": 1,
  "mode": "manage",
  "interface": "",
  "scan_interval_sec": 10,
  "signal_switch_threshold": 20,
  "connect_timeout_sec": 10,
  "cooldown_sec": 60,
  "allow_open_networks": false,
  "profiles": [
    {
      "id": "home",
      "ssid": "MyHomeNetwork",
      "priority": 100,
      "connection_name": "",
      "password": "",
      "password_file": "/root/.wifi/home.pass",
      "autoconnect": true,
      "disabled": false,
      "notes": "Primary network"
    }
  ]
}
```

### Profile Guidance

Preferred order:

1. use an existing NetworkManager connection name
2. use `password_file`
3. use inline `password` only if you accept that tradeoff

For larger fleets, keep policy in version control and keep secrets out of git by
default.

## Dashboard and API

The dashboard is a thin local UI over the same config/status files.

Main actions:

- inspect service state
- inspect visible networks
- add/edit/remove profiles
- change priorities and runtime policy
- import/merge/replace config bundles
- export full config
- trigger an immediate scan
- read recent logs

Documentation:

- [Dashboard Guide](docs/DASHBOARD.md)
- [CLI Reference](docs/CLI-REFERENCE.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)

## Operator Notes

- This tool assumes `NetworkManager`/`nmcli`.
- It is valid to run it in `observe` mode for diagnostics only.
- If your deployment does not use Wi-Fi, do not install it.
- Do not assume that changing the Wi-Fi that carries your management channel can
  be rolled out safely in one blind step across a fleet. Stage it.

## Development

### Core Validation

```bash
python3 smart_wifi_manager.py validate-config --config templates/config.json.template
python3 -m pytest tests -q
```

### Dashboard Build

```bash
cd dashboard
go build ./cmd
```

## License

Apache License 2.0
