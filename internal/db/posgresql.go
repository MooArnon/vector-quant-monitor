package db

import (
	"database/sql"
	"log/slog"

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
