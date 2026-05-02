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
- service/system health summary
- host/runtime metadata and active file paths
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

## Profile Operations

Use the `Known Profiles` panel for Wi-Fi SSID management.

The dashboard separates two concepts:

- `Available Networks` are SSIDs found by the latest scan.
- `Known Profiles` are the policy entries the service is allowed to join.

The service does not join arbitrary scanned networks. Add or update a scanned
SSID as a known profile first, then the normal priority policy decides what to
use.

### Add A Wi-Fi Network

From a scan:

1. Click `Trigger Scan`.
2. In `Available Networks`, click `Add` for the SSID.
3. Fill the password or password file if the network is secured.
4. Adjust priority if needed.
5. Click `Save Config`.
6. Click `Trigger Scan` or wait for the next service cycle.

Manually:

1. Click `Add Profile` in `Known Profiles`.
2. Fill `ID` with a stable lowercase name such as `field-router`.
3. Fill `SSID` exactly as broadcast by the access point.
4. Set `Priority`. Higher values win when multiple known networks are visible.
5. Add either `Password` or `Password File`.
6. Leave `Disabled` as `false`.
7. Click `Save Config`.
8. Click `Trigger Scan` or wait for the next scan cycle.

### Change Priority

Use either method:

- click `Up` or `Down` on the profile card
- edit the numeric `Priority` value directly

Then click `Save Config`. Click `Trigger Scan` if you want the decision
evaluated immediately.

`Prefer` or `Add top` in the scanned network table raises that SSID above the
current highest known priority. In `manage` mode this can cause the service to
switch when the SSID is reachable and the password is valid. In `observe` mode
it only updates policy and reports what would be preferred.

### Update A Password

1. Type the new password in the profile `Password` field.
2. Click `Save Config`.

If the password field is left blank, the existing stored inline password is
preserved. This prevents accidental credential deletion during normal edits.

### Disable Or Remove A Profile

- To keep the profile but stop using it, set `Disabled` to `true` and click
  `Save Config`.
- To delete it from the config, click `Remove Profile` and then `Save Config`.

On remote systems, prefer disabling first. Removing or replacing the currently
working management network can make the host unreachable.

## Import Modes

The import API and dashboard support two modes:

- `merge`
  - updates the current config with the imported bundle
  - best for additive fleet rollouts and staged profile updates
- `replace`
  - replaces the current config with the imported bundle
  - best for authoritative reset or known-clean reprovisioning

Use `replace` carefully on remote systems. If the imported bundle removes the
currently working management network, the host may become harder to reach.

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

## Fleet Use

The dashboard is a local per-host UI. It does not push a bundle to other
machines by itself.

For fleet use:

1. export or maintain a default profile bundle
2. distribute it with your fleet orchestrator
3. apply it with `merge` or `replace` depending on the rollout goal
4. keep secrets local where possible through `password_file`
