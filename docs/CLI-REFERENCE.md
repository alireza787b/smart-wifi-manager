# CLI Reference

## `install.sh`

```bash
sudo ./install.sh [OPTIONS]
```

Options:

- `--config PATH`
- `--status-file PATH`
- `--state-dir PATH`
- `--log-file PATH`
- `--dashboard-listen ADDR`
- `--dashboard-version VERSION`
- `--skip-dashboard`
- `--install-dashboard-only`

Dashboard install behavior:

- tries the matching GitHub release asset first
- falls back to local source build if the asset is unavailable
- skips dashboard setup entirely when `--skip-dashboard` is used

## `configure_smart_wifi_manager.sh`

```bash
sudo ./configure_smart_wifi_manager.sh [OPTIONS]
```

Options:

- `--config PATH`
- `--headless`
- `--import PATH`
- `--import-mode replace|merge`
- `--mode manage|observe|disabled`
- `--interface IFACE`
- `--scan-interval SEC`
- `--signal-threshold VALUE`
- `--cooldown SEC`
- `--connect-timeout SEC`
- `--allow-open-networks true|false`

Fleet/operator guidance:

- use `--import PATH --import-mode merge` for additive updates
- use `--import PATH --import-mode replace` only for authoritative reset
- prefer `password_file` in imported bundles when the same policy must be
  distributed to many hosts
- keep the secret file path stable when rotating credentials

## `smart_wifi_manager.py`

### Run

```bash
python3 smart_wifi_manager.py run \
  --config /etc/smart-wifi-manager/config.json \
  --status-file /run/smart-wifi-manager/status.json \
  --state-dir /var/lib/smart-wifi-manager \
  --log-file /var/log/smart-wifi-manager/smart-wifi-manager.log
```

Flags:

- `--once`
- `--verbose`

### Validate Config

```bash
python3 smart_wifi_manager.py validate-config --config ./templates/config.json.template
```

### Print Config

```bash
python3 smart_wifi_manager.py print-config --config /etc/smart-wifi-manager/config.json --redacted
```

### Fleet Profile Control

Profile-control commands use the shared MDS sidecar contract while remaining
standalone and local-first. They never print raw passwords when `--redacted` or
summary output is used.

```bash
python3 smart_wifi_manager.py profile list \
  --config /etc/smart-wifi-manager/config.json \
  --status-file /run/smart-wifi-manager/status.json
```

```bash
python3 smart_wifi_manager.py profile export \
  --config /etc/smart-wifi-manager/config.json
```

Profile export is redacted by default. `--include-secrets` is available only
for local private backups and should never be used in fleet reports or public
fixtures.

```bash
python3 smart_wifi_manager.py profile validate --file ./fleet-wifi.json
```

```bash
python3 smart_wifi_manager.py profile diff \
  --config /etc/smart-wifi-manager/config.json \
  --baseline ./fleet-wifi.json \
  --mode fleet-merge
```

```bash
python3 smart_wifi_manager.py profile import \
  --config /etc/smart-wifi-manager/config.json \
  --file ./fleet-wifi.json \
  --mode fleet-merge \
  --dry-run \
  --output-plan /var/lib/smart-wifi-manager/profile-control/last-plan.json
```

```bash
python3 smart_wifi_manager.py profile apply \
  --config /etc/smart-wifi-manager/config.json \
  --plan /var/lib/smart-wifi-manager/profile-control/last-plan.json \
  --confirm CONFIRMATION_TOKEN_FROM_DRY_RUN
```

```bash
python3 smart_wifi_manager.py profile promote \
  --config /etc/smart-wifi-manager/config.json \
  --output ./reference-draft.json
```

Policy modes:

- `observe`: report only; no apply mutation.
- `local`: node-local profile remains authoritative.
- `fleet-merge`: apply fleet baseline while preserving local additions.
- `fleet-strict`: authoritative baseline; advanced/lab use only.

`profile import` requires `--dry-run`. Applying changes requires
`profile apply` with the confirmation token from the dry-run plan.

## Automation Pattern

For orchestrated fleet use, treat Smart Wi-Fi Manager as a single-host runtime:

1. prepare a non-secret policy bundle
2. distribute it with your orchestration layer
3. apply it locally with `configure_smart_wifi_manager.sh`
4. inspect `/run/smart-wifi-manager/status.json` for the live result

The tool is designed to be automation-friendly, but multi-node rollout and
approval logic belong in the external fleet system.
