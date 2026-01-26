package main

import (
	"vector-quant-monitor/internal/config"
	"vector-quant-monitor/internal/monitor"
	"vector-quant-monitor/util"

	"log/slog"

	"os"
)

func main() {
	log := util.NewLogger(slog.LevelDebug.String(), "monitor")
	log.Info("Monitor started")
	config := config.LoadConfig()

	monitorTag := os.Getenv("MONITOR_TAG")
	if monitorTag == "host" {
		log.Info("Monitor Tag: " + monitorTag)
		monitor.StartMonitorHost(10*1000*1000*1000, log) // 10 seconds
	}
	if monitorTag == "binance" {
		log.Info("Monitor Tag: " + monitorTag)
		monitor.StartFuturesUserStream(
			config.Binance.ApiKey,
			config.Binance.ApiSecret,
			log,
		)
	}

	log.Info("Monitor stopped")
}
