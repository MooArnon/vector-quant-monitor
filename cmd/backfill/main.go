package main

import (
	"fmt"
	"strconv"
	"vector-quant-monitor/internal/backfill"
	"vector-quant-monitor/internal/config"
	"vector-quant-monitor/internal/db"
	"vector-quant-monitor/util"

	"log/slog"
)

const (
	LookBackDays = 1
)

func main() {
	log := util.NewLogger(slog.LevelDebug.String(), "backfill_position_history")
	log.Info("Backfill Position History started")
	config := config.LoadConfig()

	// Initialize DB
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

	history := backfill.GetPositionHistory(db, LookBackDays)
	log.Info(fmt.Sprintln("history: ", history))

	for _, item := range history {

		// 1. Convert floats to strings to match the InsertPositionHistory signature
		// Using %.2f for PnL and %.3f for Vol to match standard crypto formatting
		pnlFloat, _ := strconv.ParseFloat(item.NetPnl, 64)
		volFloat, _ := strconv.ParseFloat(item.Vol, 64)
		// pnlStr := fmt.Sprintf("%.2f", pnlFloat)
		// volStr := fmt.Sprintf("%.3f", volFloat)

		// 2. Call the insert function
		// Note: Ensure your 'open_time' column in Postgres is type TEXT or VARCHAR
		// if you want to store the string "Before History".
		// If it is TIMESTAMP, you must handle "Before History" specifically (e.g., set to null or specific date).

		err := db.InsertPositionHistory(
			db.DB,
			item.Symbol,
			item.Side,
			item.PositionSide,
			pnlFloat,
			volFloat,
			item.OpenTime,
			item.CloseTime,
		)

		if err != nil {
			fmt.Printf("Failed to insert position for %s (Time: %s): %v\n", item.Symbol, item.CloseTime, err)
		}
	}

	fmt.Println("Data insertion complete.")

	log.Info("Backfill Position History completed")
}
