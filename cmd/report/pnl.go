package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"vector-quant-monitor/internal/config"
	"vector-quant-monitor/internal/db"
	"vector-quant-monitor/util"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/jung-kurt/gofpdf"
	_ "github.com/lib/pq"
)

type TradeRecord struct {
	Symbol         string
	PositionOpenAt string
	NetPnL         float64
	SigSide        sql.NullString
	Reason         sql.NullString
	CandlePrefix   sql.NullString
	ChartPrefix    sql.NullString
}

func main() {
	// 1. Setup Config & Logger
	cfg := config.LoadConfig()
	logger := util.NewLogger(slog.LevelDebug.String(), "Generate report")

	dbConnectionString := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable&timezone=Asia/Bangkok",
		cfg.Database.DBUser,
		cfg.Database.DBPassword,
		cfg.Database.DBHost,
		fmt.Sprintf("%d", cfg.Database.DBPort),
		cfg.Database.DBName,
	)

	database := db.NewPostgreSQLDB(dbConnectionString, logger)

	// 2. Query (ใช้นามแฝง p และ s เพื่อเลี่ยง reserved keywords)
	query := `
        SELECT 
            p.symbol,
            p.open_timestamp::text as position_open_at,
            p.net_pnl,
            s.side AS sig_side,
            s.reason,
            s.candle_prefix,
            s.chart_prefix
        FROM trading.position_history AS p
        JOIN trading.signal_log AS s ON p.symbol = s.symbol
            AND TO_TIMESTAMP(s.recorded_at) AT TIME ZONE 'Asia/Bangkok' < p.open_timestamp
            AND TO_TIMESTAMP(s.recorded_at) AT TIME ZONE 'Asia/Bangkok' > p.open_timestamp - INTERVAL '15 minutes'
        ORDER BY p.recorded_at DESC LIMIT 100
    `

	rows, err := database.DB.Query(query)
	if err != nil {
		logger.Error("Failed to execute query", "error", err)
		return
	}
	defer rows.Close()

	// 3. Setup PDF & AWS
	pdf := gofpdf.New("P", "mm", "A4", "")

	fmt.Println("Starting PDF generation...")
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String("ap-southeast-1"), // ปรับตาม Zone ของคุณ
	}))

	count := 0
	for rows.Next() {
		var r TradeRecord
		if err := rows.Scan(&r.Symbol, &r.PositionOpenAt, &r.NetPnL, &r.SigSide, &r.Reason, &r.CandlePrefix, &r.ChartPrefix); err != nil {
			continue
		}

		// ส่ง count เข้าไปเพื่อตัดสินใจว่าจะขึ้นหน้าใหม่ หรือแค่ครึ่งล่าง
		renderTradeRow(pdf, r, sess, count)
		count++
	}

	// เช็ค error ที่อาจเกิดขึ้นระหว่าง streaming rows
	if err = rows.Err(); err != nil {
		logger.Error("Iteration error", "error", err)
	}

	fmt.Printf("Total records found: %d\n", count)

	if count > 0 {
		err = pdf.OutputFileAndClose("trading_optimization_report.pdf")
		if err != nil {
			logger.Error("Failed to save PDF", "error", err)
		} else {
			fmt.Println("Report saved as: trading_optimization_report.pdf")
		}
	} else {
		fmt.Println("No records found within the timeframe to generate report.")
	}
}

func renderTradeRow(pdf *gofpdf.Fpdf, r TradeRecord, sess *session.Session, index int) {
	var yOffset float64

	// ถ้า index เป็นเลขคู่ (0, 2, 4...) ให้ขึ้นหน้าใหม่และเริ่มที่ด้านบน
	if index%2 == 0 {
		pdf.AddPage()
		yOffset = 10
	} else {
		// ถ้าเป็นเลขคี่ (1, 3, 5...) ให้เริ่มที่ครึ่งหน้า (ประมาณ 150mm)
		yOffset = 150
		// วาดเส้นแบ่งครึ่งหน้า (Optional)
		pdf.SetDrawColor(200, 200, 200)
		pdf.Line(10, 145, 200, 145)
	}

	pdf.SetY(yOffset)

	// --- ส่วนการวาด Content (เหมือนเดิมแต่ใช้ Relative Y) ---
	pdf.SetFont("Arial", "B", 12)
	if r.NetPnL < 0 {
		pdf.SetTextColor(200, 0, 0)
		pdf.CellFormat(0, 8, fmt.Sprintf("[LOSS] %s | PnL: %.2f", r.Symbol, r.NetPnL), "", 1, "L", false, 0, "")
	} else {
		pdf.SetTextColor(0, 120, 0)
		pdf.CellFormat(0, 8, fmt.Sprintf("[WIN] %s | PnL: %.2f", r.Symbol, r.NetPnL), "", 1, "L", false, 0, "")
	}

	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 9)
	pdf.Cell(0, 5, fmt.Sprintf("Time: %s | Side: %s", r.PositionOpenAt, r.SigSide.String))
	pdf.Ln(6)

	// Reason Box (จำกัดความสูงเพื่อไม่ให้เบียดกัน)
	pdf.SetFillColor(245, 245, 245)
	pdf.SetFont("Arial", "I", 8)
	if r.Reason.Valid {
		// จำกัดความกว้าง 190mm และวาด Reason
		pdf.MultiCell(190, 4, "Reason: "+r.Reason.String, "1", "L", true)
	}

	currentY := pdf.GetY() + 5

	// วางรูปภาพขนานกัน (ลดขนาดลงเล็กน้อยเพื่อให้ลงตัว)
	imgWidth := 92.0
	if r.CandlePrefix.Valid && r.CandlePrefix.String != "" {
		downloadAndDraw(pdf, sess, r.CandlePrefix.String, 10, currentY, imgWidth, "Candle Chart")
	}
	if r.ChartPrefix.Valid && r.ChartPrefix.String != "" {
		downloadAndDraw(pdf, sess, r.ChartPrefix.String, 107, currentY, imgWidth, "Trend Chart")
	}
}

// ปรับฟังก์ชัน downloadAndDraw ให้รับความกว้าง (width) ได้
func downloadAndDraw(pdf *gofpdf.Fpdf, sess *session.Session, key string, x, y, w float64, label string) {
	downloader := s3manager.NewDownloader(sess)
	tmpFile, _ := os.CreateTemp("", "trade_*.png")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	_, err := downloader.Download(tmpFile, &s3.GetObjectInput{
		Bucket: aws.String("vector-quant-trader-log"),
		Key:    aws.String(key),
	})

	if err == nil {
		pdf.SetFont("Arial", "B", 7)
		pdf.SetXY(x, y-3)
		pdf.Cell(0, 0, label)
		// ใช้ width (w) ที่ส่งเข้ามา
		pdf.ImageOptions(tmpFile.Name(), x, y, w, 0, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	}
}
