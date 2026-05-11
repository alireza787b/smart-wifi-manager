# Dashboard Guide

Default listen address:

- `127.0.0.1:9080`

## Overview

![Smart Wi-Fi Manager dashboard](images/dashboard-overview.png)

The screenshot uses synthetic demo data. Public documentation must not expose
customer SSIDs, hostnames, IP addresses, passwords, or service logs.

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
3. The profile dialog opens with the SSID, security, signal, and suggested
   priority.
4. Fill the `Password` field or `Password File` if the network is secured.
5. Click `Save Profile`, or `Save & Scan Now` if you want the service to
   evaluate the profile immediately.

`Add top` opens the same dialog with a higher suggested priority. Use it when
the new network should win over other visible known networks.

Manually:

1. Click `Add Manual SSID` in `Known Profiles`.
2. The profile dialog opens.
3. Fill `SSID` exactly as broadcast by the access point.
4. Set `Priority`. Higher values win when multiple known networks are visible.
5. Add either `Password` or `Password File` for secured networks.
6. Leave `Disabled` as `false`.
7. Click `Save Profile`, or `Save & Scan Now`.

### Change Priority

Use one of these actions on the profile card:

- `Up` or `Down` changes the numeric priority and saves immediately.
- `Prefer` raises that profile above all other known profiles and triggers a
  scan immediately.

`Prefer` or `Add top` in the scanned network table raises that SSID above the
current highest known priority. In `manage` mode this can cause the service to
switch when the SSID is reachable and the password is valid. In `observe` mode
it only updates policy and reports what would be preferred.

### Update A Password

1. Click `Edit` on the known profile.
2. Type the new password in the profile `Password` field.
3. Click `Save Profile`, or `Save & Scan Now`.

If the password field is left blank, the existing stored inline password is
preserved. This prevents accidental credential deletion during normal edits.

### Disable Or Remove A Profile

- To keep the profile but stop using it, click `Edit`, set `Disabled` to
  `true`, then save.
- To delete it from the config, click `Remove` and confirm. Removal saves
  immediately.

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
| `/api/config/export` | `GET` | Export redacted config |
| `/api/config/import?mode=merge|replace` | `POST` | Import bundle |
| `/api/actions/scan` | `POST` | Trigger immediate scan |
| `/api/logs` | `GET` | Recent log lines |

Fleet profile-control API:

Remote mutating requests require `SMART_WIFI_MANAGER_API_TOKEN` and must send
the value with `Authorization: Bearer ...` or `X-SWM-Profile-Token`. Loopback
requests remain usable for local standalone operation when no token is set.

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/profiles/summary` | `GET` | Redacted profile summary, sanitized hash, service context, and secret status |
| `/api/v1/profiles/export` | `GET` | Redacted profile export |
| `/api/v1/profiles/validate` | `POST` | Validate a candidate profile bundle |
| `/api/v1/profiles/diff` | `POST` | Compare candidate baseline against local profile |
| `/api/v1/profiles/import` | `POST` | Create a dry-run import plan; requires `dry_run=true` |
| `/api/v1/profiles/apply` | `POST` | Apply a stored dry-run plan with confirmation token |
| `/api/v1/profiles/promote-reference-draft` | `POST` | Return a sanitized reference draft from this node |

Supported profile policy modes:

- `observe`
- `local`
- `fleet-merge`
- `fleet-strict`

Every mutating fleet profile workflow is dry-run first and apply second.
`fleet-merge` preserves local profiles that are not in the baseline.
`fleet-strict` is advanced/lab mode and requires an additional confirmation.

## Secret Handling

- `password_file` paths are shown as paths
- inline passwords are redacted from `GET /api/config`
- v1 profile-control endpoints summarize secrets only as `stored`, `missing`,
  `external file`, or `redacted`
- secret fields such as passwords, PSKs, API keys, tokens, and private keys are
  recursively redacted from profile-control summaries and exports
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

In MDS deployments, MDS owns the fleet rollout: the Smart Wi-Fi release/ref and
repo-owned profile are pinned in the MDS repo, then node git sync/reconcile
updates each managed companion. Do not add a separate self-update workflow to
this dashboard.
