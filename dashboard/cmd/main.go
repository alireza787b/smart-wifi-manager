package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/alireza787b/smart-wifi-manager/dashboard/internal/api"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9080", "Address to listen on")
	configPath := flag.String("config", "/etc/smart-wifi-manager/config.json", "Path to smart-wifi-manager config")
	statusPath := flag.String("status", "/run/smart-wifi-manager/status.json", "Path to smart-wifi-manager status JSON")
	controlDir := flag.String("control-dir", "/var/lib/smart-wifi-manager/control", "Control directory")
	logFile := flag.String("log-file", "/var/log/smart-wifi-manager/smart-wifi-manager.log", "Service log file")
	version := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *version {
		fmt.Printf("smart-wifi-manager dashboard %s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	server := api.NewServer(api.Options{
		ConfigPath: *configPath,
		StatusPath: *statusPath,
		ControlDir: *controlDir,
		LogPath:    *logFile,
		Version:    Version,
	})

	go func() {
		log.Printf("smart-wifi-manager dashboard %s starting on http://%s", Version, *listen)
		if err := http.ListenAndServe(*listen, server.Router()); err != nil {
			log.Fatalf("dashboard failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down smart-wifi-manager dashboard...")
}
