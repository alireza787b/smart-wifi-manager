## Smart Wi-Fi Manager

Smart Wi-Fi Manager is a Bash script and systemd service for Raspberry Pi devices that ensures they are always connected to the strongest known Wi-Fi network. It scans for Wi-Fi networks periodically, compares signal strengths, and switches to a stronger network if available.

## Features

- **Automatic Connection:** Connects to the strongest available known Wi-Fi network.
- **Periodic Scanning:** Scans for Wi-Fi networks every 10 seconds (configurable).
- **Easy Configuration:** Edit a configuration file to add your known networks.
- **Service Integration:** Runs as a background service using `systemd`.
- **Real-time Updates:** Reads the configuration file on each scan for immediate effect.
- **Logging:** Keeps logs for monitoring and debugging.

## Prerequisites
- Root privileges to install and run the script as a service.

- Raspberry Pi running a Linux distribution with `nmcli` (NetworkManager) installed. Before installing Smart Wi-Fi Manager, make sure `NetworkManager` and `nmcli` are available.

```bash
sudo apt update
sudo apt install network-manager
```

**Note:** If you're using an older Raspberry Pi OS version or `dhcpcd`, you may need to switch to NetworkManager. If you're on a recent version (Bullseye or newer), you're all set!

### Switching to NetworkManager if using older version

1. **Stop and Disable DHCPCD:**
   ```bash
   sudo systemctl stop dhcpcd
   sudo systemctl disable dhcpcd
   ```

2. **Enable and Start NetworkManager:**
   ```bash
   sudo systemctl enable NetworkManager
   sudo systemctl start NetworkManager
   ```

**Caution:** When disabling dhcpcd, ensure that you have a backup connection method (like an Ethernet cable or serial or mouse and keyboard) ready, as this may temporarily disconnect your device from the network. After switching, reconnect to your Wi-Fi networks through NetworkManager settings.



## Installation

### 1. Clone the Repository

```bash
git clone https://github.com/alireza787b/smart-wifi-manager.git
```

### 2. Navigate to the Directory

```bash
cd smart-wifi-manager
```

### 3. Edit Known Networks Configuration

Edit the `known_networks.conf` file to include your known Wi-Fi networks:

```conf
# known_networks.conf
# Format:
# ssid=<Your_SSID>
# password=<Your_Password>

# Example:
ssid=MyHomeNetwork
password=SuperSecretPassword

ssid=OfficeNetwork
password=AnotherSecretPassword
```

### 4. Modify the Service Parameters
Open the `smart-wifi-manager.service` and edit the following lines depending on your user and path settings. (eg. pi, /home/pi/) 
```bash
User=root
ExecStart=/bin/bash /path/to/smart-wifi-manager/smart-wifi-manager.sh
WorkingDirectory=/path/to/smart-wifi-manager
```

### 5. Install the Script and Service

Run the installation script:

```bash
sudo bash install.sh
```

### 6. Verify the Service Status

Check if the service is running:

```bash
sudo systemctl status smart-wifi-manager.service
```

## Usage

- **Running as a Service:** The script runs in the background as a `systemd` service.
- **Logs:** Logs are stored in the same directory as the script (`smart-wifi-manager.log`).
- **Configuration Updates:** To update known networks, edit `known_networks.conf` and the changes will be applied automatically on the next scan.

## Security Notice

**Important:** The `known_networks.conf` file stores your Wi-Fi passwords in plain text. To ensure the security of your networks:

- **Secure File Permissions:** Ensure that the configuration file is readable only by authorized users.

  ```bash
  chmod 600 known_networks.conf
  ```

- **Network Security Measures:** Consider implementing MAC address filtering or other network security measures on your router.

- **Encryption (Advanced):** If you require encryption of the passwords, you will need to implement encryption and decryption mechanisms within the script.

## Service Management

### Viewing Logs

To view the service logs:

```bash
sudo journalctl -u smart-wifi-manager.service -f
```

### Restarting the Service

To restart the service:

```bash
sudo systemctl restart smart-wifi-manager.service
```

### Checking Service Status

To check the status of the service:

```bash
sudo systemctl status smart-wifi-manager.service
```

### Stopping the Service

To stop the service:

```bash
sudo systemctl stop smart-wifi-manager.service
```

## Updating Known Networks

To add or remove known networks, edit the `known_networks.conf` file in your cloned repository. No need to restart the service; it reads the configuration file each time it scans.

## Uninstallation

To stop and disable the service:

```bash
sudo systemctl stop smart-wifi-manager.service
sudo systemctl disable smart-wifi-manager.service
```

To remove the service file:

```bash
sudo rm /etc/systemd/system/smart-wifi-manager.service
sudo systemctl daemon-reload
```


### **Technical and Non-Technical Explanation:**

**Technical Explanation:**

The Smart Wi-Fi Manager is a Bash script designed to enhance the Wi-Fi connectivity of Raspberry Pi devices. It operates by:

- **Scanning Wi-Fi Networks:** Uses `nmcli` to scan for available Wi-Fi networks.
- **Loading Known Networks:** Reads from `known_networks.conf` to get a list of SSIDs and passwords.
- **Evaluating Signal Strengths:** Compares the signal strengths of available networks against the current connection.
- **Switching Networks:** If a stronger known network is found and the signal improvement exceeds a predefined threshold, the script switches to that network.
- **Running as a Service:** The script is set up as a `systemd` service for continuous background operation.
- **Logging:** All activities are logged for monitoring and troubleshooting.

**Non-Technical Explanation:**

Smart Wi-Fi Manager is a tool that helps your Raspberry Pi always connect to the best Wi-Fi network you've set up. Here's what it does:

- **Automatic Connection:** It automatically finds and connects to the Wi-Fi network with the strongest signal among those you trust.
- **Easy Setup:** You list your Wi-Fi networks and their passwords in a simple file, and the tool takes care of the rest.
- **Background Operation:** Once installed, it runs in the background, so you don't have to manually manage your Wi-Fi connections.
- **Real-time Updates:** If you add or remove networks from your list, the tool will automatically recognize the changes.
- **Reliability:** Ensures your device stays connected to the best possible network, improving internet connectivity and performance.

## Contributing

Contributions are welcome! Please fork the repository and submit a pull request for any improvements or bug fixes.

## License

This project is licensed under the [Apache License 2.0](LICENSE).

## Support

If you encounter any issues or have questions, feel free to open an issue in the repository.


