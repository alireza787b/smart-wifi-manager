# Troubleshooting

## `nmcli` missing

Install NetworkManager:

```bash
sudo apt update
sudo apt install network-manager
```

## No Wi-Fi interface detected

Check what NetworkManager sees:

```bash
nmcli device status
```

If your board uses Ethernet-only or LTE-only networking, this tool may simply
not be applicable. Leave it uninstalled.

## Dashboard is up but status is empty

Check the service:

```bash
sudo systemctl status smart-wifi-manager.service
cat /run/smart-wifi-manager/status.json
```

Then inspect logs:

```bash
sudo journalctl -u smart-wifi-manager.service -f
tail -n 200 /var/log/smart-wifi-manager/smart-wifi-manager.log
```

## Config saves but behavior does not change

The service reloads config every scan cycle. For immediate confirmation:

```bash
sudo systemctl restart smart-wifi-manager.service
```

Or trigger a scan from the dashboard/API.

## Secured SSID fails with `key-mgmt` missing

Older or manually created NetworkManager profiles can miss the WPA key-management
field even when the Smart Wi-Fi profile has a password. Current releases repair
that connection before activation by setting `wpa-psk` and the stored PSK.

If the error persists, inspect and remove the stale connection profile:

```bash
nmcli connection show
sudo nmcli connection delete "<connection-name>"
sudo systemctl restart smart-wifi-manager.service
```

## Wi-Fi credentials changed and nodes disappeared

That is an operational rollout problem, not a software bug by itself.

Use a staged migration strategy:

- overlap old and new SSIDs during transition
- change one subset first
- keep an out-of-band path (Ethernet, serial, VPN, RC ops workflow, local console)

Do not assume a single blind credential flip is safe across a remote fleet.
