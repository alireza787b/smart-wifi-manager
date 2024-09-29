#!/bin/bash

# Installation script for Smart Wi-Fi Manager

set -e

SERVICE_NAME="smart-wifi-manager.service"

# Get the directory of the script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Make the script executable
chmod +x "$SCRIPT_DIR/smart-wifi-manager.sh"

# Detect the absolute path to the smart-wifi-manager.sh script
SCRIPT_PATH="$SCRIPT_DIR/smart-wifi-manager.sh"

# Update the service file with the correct path
sed -i "s|/path/to/smart-wifi-manager|$SCRIPT_DIR|g" "$SCRIPT_DIR/$SERVICE_NAME"

# Copy the service file to systemd directory
sudo cp "$SCRIPT_DIR/$SERVICE_NAME" /etc/systemd/system/

# Reload systemd daemon
sudo systemctl daemon-reload

# Enable and start the service
sudo systemctl enable smart-wifi-manager.service
sudo systemctl start smart-wifi-manager.service

echo "Installation completed successfully."
