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
