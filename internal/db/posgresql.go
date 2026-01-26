package db

import (
	"database/sql"
	"log/slog"
	"time"

	_ "github.com/lib/pq"
)

type Postgresql struct {
	DB *sql.DB
}

func NewPostgreSQLDB(connectionString string, log *slog.Logger) *Postgresql {
	// Connection string format: "postgres://user:password@host:port/dbname?sslmode=disable"
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		return nil
	}
	return &Postgresql{DB: db}
}

func CloseDB(db *sql.DB) error {
	return db.Close()
}

func (p *Postgresql) InsertHostMetrics(db *sql.DB, cpuPercent float64, ramPercent float64, diskPercent float64) error {
	// TODO Change host to parameterized value it could be the name of ec2 node
	query := `
		INSERT INTO system_metric (recorded_at, resource, cpu_pct, mem_pct, disk_pct)
		VALUES (current_timestamp, 'ec2-host', $1, $2, $3)
	`
	_, err := p.DB.Exec(query, cpuPercent, ramPercent, diskPercent)
	return err
}

func (p *Postgresql) InsertPositionHistory(
	db *sql.DB,
	symbol string,
	side string,
	positionSide string,
	netPnl float64,
	vol float64,
	openTime time.Time,
	closeTime time.Time,
) error {
	query := `
		INSERT INTO trading.position_history (
			recorded_at
			, market
			, symbol
			, side
			, net_pnl
			, volume
			, open_timestamp
			, close_timestamp
		)
		VALUES (
			current_timestamp
			, 0
			, $1
			, $2
			, $3
			, $4
			, $5
			, $6
		)
		ON CONFLICT (open_timestamp, symbol) DO NOTHING
	`
	_, err := p.DB.Exec(query, symbol, side, netPnl, vol, openTime, closeTime)
	return err
}
