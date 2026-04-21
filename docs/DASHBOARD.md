# Dashboard Guide

Default listen address:

- `127.0.0.1:9080`

## Overview

![Smart Wi-Fi Manager dashboard](images/dashboard-overview.png)

Expose remotely only when you intend to:

```bash
sudo ./install.sh --dashboard-listen 0.0.0.0:9080
```

## What The Dashboard Shows

- current service mode
- current Wi-Fi connection
- available networks from the latest scan
- known profiles and effective priority
- warnings such as:
  - `nmcli` missing
  - no Wi-Fi interface
  - NetworkManager inactive
  - profile connection failures
- recent service logs

## What The Dashboard Changes

- writes to `/etc/smart-wifi-manager/config.json`
- reads from `/run/smart-wifi-manager/status.json`
- triggers immediate scan by touching the control file in:
  - `/var/lib/smart-wifi-manager/control/scan-now`

The service itself reloads config every cycle, so a manual “reload config”
button is not required.

## API Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/meta` | `GET` | Paths and version |
| `/api/status` | `GET` | Latest runtime status |
| `/api/config` | `GET` | Redacted config |
| `/api/config` | `PUT` | Replace current config from UI save |
| `/api/config/export` | `GET` | Export full config |
| `/api/config/import?mode=merge|replace` | `POST` | Import bundle |
| `/api/actions/scan` | `POST` | Trigger immediate scan |
| `/api/logs` | `GET` | Recent log lines |

## Secret Handling

- `password_file` paths are shown as paths
- inline passwords are redacted from `GET /api/config`
- if the UI saves a profile with blank password, the existing inline password is preserved
- explicit secret deletion should be handled by editing the config bundle or adding UI support later
