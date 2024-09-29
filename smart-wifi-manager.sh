#!/bin/bash

# Smart Wi-Fi Manager Script for Raspberry Pi
# This script ensures Raspberry Pi devices are always connected to the strongest known Wi-Fi network.
# It scans for Wi-Fi networks periodically, compares signal strength, and switches to a stronger network if available.

# =======================
# Configuration Parameters
# =======================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="$SCRIPT_DIR/known_networks.conf"   # Path to known networks configuration file
LOG_FILE="$SCRIPT_DIR/smart-wifi-manager.log"   # Log file to record all activities
SCAN_INTERVAL=10                                # Time (in seconds) between Wi-Fi scans
SIGNAL_THRESHOLD=20                             # Minimum signal strength improvement to trigger a switch
MAX_LOG_SIZE=5242880                            # Maximum log file size (5 MB)
BACKUP_COUNT=3                                  # Number of rotated log files to keep
LOCK_FILE="/var/run/smart-wifi-manager.lock"    # Lock file to prevent multiple instances

# =======================
# Logging Function
# =======================

log() {
    local level="$1"
    shift
    local message="$@"
    printf "%s [%s] %s\n" "$(date '+%Y-%m-%d %H:%M:%S')" "$level" "$message" | tee -a "$LOG_FILE"
}

# =======================
# Initial Setup and Checks
# =======================

# Ensure the script runs with root privileges
if [ "$EUID" -ne 0 ]; then
    log "ERROR" "Please run as root."
    exit 1
fi

# Create and acquire a lock to prevent multiple instances of the script from running
exec 200>"$LOCK_FILE"
flock -n 200 || { log "ERROR" "Another instance of the script is running."; exit 1; }

# Ensure the lock file is removed on script exit
trap 'rm -f "$LOCK_FILE"' EXIT

# Create log directory if it doesn't exist
mkdir -p "$(dirname "$LOG_FILE")"

# =======================
# Log Rotation Function
# =======================

rotate_logs() {
    if [ -f "$LOG_FILE" ]; then
        local log_size
        log_size=$(stat -c%s "$LOG_FILE")
        if [ "$log_size" -ge "$MAX_LOG_SIZE" ]; then
            # Rotate logs and keep backups
            for ((i=BACKUP_COUNT; i>=1; i--)); do
                if [ -f "$LOG_FILE.$i" ]; then
                    if [ "$i" -eq "$BACKUP_COUNT" ]; then
                        rm -f "$LOG_FILE.$i"
                    else
                        mv "$LOG_FILE.$i" "$LOG_FILE.$((i+1))"
                    fi
                fi
            done
            mv "$LOG_FILE" "$LOG_FILE.1"
            : > "$LOG_FILE"  # Truncate current log file
        fi
    else
        : > "$LOG_FILE"  # Create log file if it doesn't exist
    fi
}

# =======================
# Load Known Networks Function
# =======================

load_known_networks() {
    if [ ! -f "$CONFIG_FILE" ]; then
        log "ERROR" "Configuration file $CONFIG_FILE not found."
        exit 1
    fi

    declare -gA KNOWN_NETWORKS  # Declare global associative array for SSIDs and passwords
    local ssid=""
    local password=""

    log "INFO" "Loading known networks from configuration file..."

    while IFS='=' read -r key value || [ -n "$key" ]; do
        key=$(echo "$key" | xargs)    # Trim leading/trailing whitespace
        value=$(echo "$value" | xargs)

        # Skip empty lines or comments
        if [[ -z "$key" ]] || [[ "$key" == \#* ]]; then
            continue
        fi

        # Parse SSID and password pairs from the configuration file
        case "$key" in
            ssid)
                ssid="$value"
                ;;
            password)
                password="$value"
                if [ -n "$ssid" ]; then
                    KNOWN_NETWORKS["$ssid"]="$password"
                    log "INFO" "Loaded network: SSID='$ssid'"
                    ssid=""
                    password=""
                fi
                ;;
        esac
    done < "$CONFIG_FILE"

    if [ ${#KNOWN_NETWORKS[@]} -eq 0 ]; then
        log "ERROR" "No known networks loaded from $CONFIG_FILE."
        exit 1
    fi
}

# =======================
# Get Wi-Fi Interface Function
# =======================

get_wifi_interface() {
    local interfaces
    interfaces=$(nmcli device status | awk '$2 == "wifi" {print $1}' | head -n1)
    if [ -z "$interfaces" ]; then
        log "ERROR" "No Wi-Fi interface found."
        exit 1
    fi
    printf "%s" "$interfaces"
}

# Get the wireless interface for later use
INTERFACE=$(get_wifi_interface)
log "INFO" "Using Wi-Fi interface: $INTERFACE"

# =======================
# Scan Wi-Fi Networks Function
# =======================

scan_wifi_networks() {
    available_networks=()
    local scan_output
    log "INFO" "Scanning for available Wi-Fi networks..."

    # Use terse output with colon as delimiter
    scan_output=$(nmcli -t -f SSID,SIGNAL dev wifi list ifname "$INTERFACE" --rescan yes 2>&1)

    if [ $? -ne 0 ] || [ -z "$scan_output" ]; then
        log "WARNING" "Failed to scan Wi-Fi networks on interface '$INTERFACE'. Output: $scan_output"
        return
    fi

    log "INFO" "Available networks:"

    # Parse SSID and signal strength using colon as delimiter
    while IFS=: read -r ssid signal; do
        # Trim any leading/trailing whitespace
        ssid=$(echo "$ssid" | xargs)
        signal=$(echo "$signal" | xargs)

        # Skip empty SSIDs (hidden networks)
        if [ -z "$ssid" ]; then
            continue
        fi

        available_networks+=("$ssid;$signal")
        log "INFO" "Found network: SSID='$ssid', Signal='$signal%'"
    done <<< "$scan_output"
}

# =======================
# Get Current Connection Info Function
# =======================

get_current_connection_info() {
    current_ssid=$(nmcli -t -f ACTIVE,SSID dev wifi | grep '^yes:' | cut -d':' -f2-)
    if [ -n "$current_ssid" ]; then
        current_signal=$(nmcli -t -f ACTIVE,SIGNAL dev wifi | grep '^yes:' | cut -d':' -f2)
        log "INFO" "Currently connected to '$current_ssid' with signal strength $current_signal%."
    else
        current_ssid=""
        current_signal=0
        log "INFO" "Not connected to any network."
    fi
}

# =======================
# Connect to Network Function Using nmcli
# =======================

connect_to_network() {
    local ssid="$1"
    local password="$2"
    local timeout=10  # Set a timeout of 10 seconds for the connection attempt

    log "INFO" "Attempting to connect to network: SSID='$ssid'"

    if [ -z "$password" ]; then
        nmcli_output=$(timeout "$timeout" nmcli dev wifi connect "$ssid" ifname "$INTERFACE" 2>&1)
    else
        nmcli_output=$(timeout "$timeout" nmcli dev wifi connect "$ssid" password "$password" ifname "$INTERFACE" 2>&1)
    fi
    nmcli_exit_status=$?

    if [ "$nmcli_exit_status" -eq 0 ]; then
        log "INFO" "Successfully connected to '$ssid'."
        return 0
    elif [ "$nmcli_exit_status" -eq 124 ]; then  # 124 is the exit code when timeout is reached
        log "ERROR" "Connection attempt to '$ssid' timed out after $timeout seconds."
    else
        log "ERROR" "Failed to connect to '$ssid'. Output: $nmcli_output"
    fi
    return 1
}

# =======================
# Main Logic Loop
# =======================

main_loop() {
    while true; do
        rotate_logs
        load_known_networks
        scan_wifi_networks
        get_current_connection_info

        local best_ssid=""
        local best_signal=-100

        # Find the best available network based on signal strength
        log "INFO" "Evaluating networks for best connection..."
        for entry in "${available_networks[@]}"; do
            local ssid
            local signal
            ssid=$(echo "$entry" | cut -d';' -f1)
            signal=$(echo "$entry" | cut -d';' -f2)

            # Ensure signal is a valid number
            if ! [[ "$signal" =~ ^-?[0-9]+$ ]]; then
                log "WARNING" "Invalid signal strength for SSID='$ssid'. Skipping..."
                continue
            fi

            # Check if this SSID is a known network and has a stronger signal
            if [[ -v "KNOWN_NETWORKS[$ssid]" ]]; then
                log "INFO" "SSID='$ssid' is a known network."
                if [ "$signal" -gt "$best_signal" ]; then
                    log "INFO" "SSID='$ssid' has a better signal ($signal%) compared to current best ($best_signal%)."
                    best_signal="$signal"
                    best_ssid="$ssid"
                    best_password="${KNOWN_NETWORKS[$ssid]}"
                fi
            else
                log "INFO" "SSID='$ssid' is not in the list of known networks. Skipping..."
            fi
        done

        # Decision-making based on the best available network
        if [ "$current_ssid" != "$best_ssid" ] && [ -n "$best_ssid" ]; then
            signal_diff=$((best_signal - current_signal))
            if [ "$signal_diff" -ge "$SIGNAL_THRESHOLD" ]; then
                log "INFO" "Decided to switch to better network '$best_ssid' (Signal: $best_signal%, Improvement: $signal_diff%)."
                if ! connect_to_network "$best_ssid" "$best_password"; then
                    log "WARNING" "Failed to switch to network '$best_ssid'. Retrying..."
                fi
            else
                log "INFO" "Signal improvement ($signal_diff%) is less than the threshold ($SIGNAL_THRESHOLD%). Not switching."
            fi
        elif [ -z "$current_ssid" ] && [ -n "$best_ssid" ]; then
            log "INFO" "Currently disconnected. Attempting to connect to best network '$best_ssid' (Signal: $best_signal%)."
            if ! connect_to_network "$best_ssid" "$best_password"; then
                log "WARNING" "Failed to connect to network '$best_ssid'. Retrying..."
            fi
        else
            log "INFO" "No better network found. Staying connected to '$current_ssid'."
        fi

        sleep "$SCAN_INTERVAL"  # Wait before next scan
    done
}

# =======================
# Start the Script
# =======================

main_loop  # Start the main loop for continuously checking Wi-Fi status and switching networks
