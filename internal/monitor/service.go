package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"

	"vector-quant-monitor/internal/config"
	"vector-quant-monitor/internal/db"
)

func StartMonitorHost(interval time.Duration, log *slog.Logger) error {
	config := config.LoadConfig()
	log.Info(
		fmt.Sprintf("Connecting to PostgresqldDB to %s",
			config.Database.DBName,
		),
	)
	dbConnectionString := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		config.Database.DBUser,
		config.Database.DBPassword,
		config.Database.DBHost,
		fmt.Sprintf("%d", config.Database.DBPort),
		config.Database.DBName,
	)
	db := db.NewPostgreSQLDB(
		dbConnectionString,
		log,
	)
	if db == nil {
		log.Info("Failed to connect to DB")
	}
	defer db.DB.Close()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// READ DISK PATH: If running in Docker, this should be "/rootfs"
	hostDiskPath := os.Getenv("HOST_DISK_PATH")
	if hostDiskPath == "" {
		hostDiskPath = "/"
	}

	log.Info(fmt.Sprintf("Monitoring Host Disk at: %s", hostDiskPath))

	for range ticker.C {
		// 1. HOST MEMORY
		// gopsutil automatically uses HOST_PROC env var to read /host/proc/meminfo
		v, err := mem.VirtualMemory()
		if err != nil {
			log.Info(fmt.Sprintf("Error reading memory: %v", err))
			continue
		}

		// 2. HOST CPU
		// gopsutil automatically uses HOST_PROC env var to read /host/proc/stat
		c, err := cpu.Percent(0, false)
		if err != nil {
			log.Info(fmt.Sprintf("Error reading CPU: %v", err))
			continue
		}

		// 3. HOST DISK
		// We explicitly ask for usage of the mounted path
		d, err := disk.Usage(hostDiskPath)
		if err != nil {
			log.Info(fmt.Sprintf("Error reading disk: %v", err))
			continue
		}

		// EXTRACT PERCENTAGES
		cpuPercent := c[0]
		ramPercent := v.UsedPercent
		diskPercent := d.UsedPercent

		dbErr := db.InsertHostMetrics(db.DB, cpuPercent, ramPercent, diskPercent)
		if dbErr != nil {
			log.Info(fmt.Sprintf("Error inserting metrics to DB: %v", dbErr))
			continue
		}

		// LOGGING (For verification)
		// If RAM matches your EC2 size (e.g. 2GB/4GB) instead of container limit, it works.
		log.Info(fmt.Sprintf("HOST STATS -> CPU: %.1f%% | RAM: %.1f%% (Total: %.0fMB) | Disk: %.1f%%",
			cpuPercent, ramPercent, float64(v.Total)/1024/1024, diskPercent))
	}

	return nil
}
