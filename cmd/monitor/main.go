package main

import (
	"vector-quant-monitor/internal/monitor"
	"vector-quant-monitor/util"

	"log/slog"

	"os"
)

func main() {
	log := util.NewLogger(slog.LevelDebug.String(), "monitor")
	log.Info("Monitor started")

	monitorTag := os.Getenv("MONITOR_TAG")
	if monitorTag == "host" {
		log.Info("Monitor Tag: " + monitorTag)
		monitor.StartMonitorHost(10*1000*1000*1000, log) // 10 seconds
	}

	log.Info("Monitor stopped")
}
