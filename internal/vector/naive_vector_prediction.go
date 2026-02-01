package vector

import (
	"fmt"
	"log/slog"
	"math"
	"time"
	"vector-quant-monitor/internal/config"
	"vector-quant-monitor/internal/db"

	// "encoding/json" // No longer needed with pgvector.Vector
	"github.com/pgvector/pgvector-go"
)

type QueryRandomRow struct {
	Embedding  []float64
	NextSlope5 float64
}

type PatternLabel struct {
	Time       time.Time `json:"time"`
	Symbol     string    `json:"symbol"`
	Interval   string    `json:"interval"`
	NextReturn float64   `json:"next_return"`
	NextSlope3 float64   `json:"next_slope_3"`
	NextSlope5 float64   `json:"next_slope_5"`
	Embedding  []float64
	Distance   float64
}

type PredictionResult struct {
	PositiveCount int
	NegativeCount int
	IsCorrect     bool
	NumDiffCount  float64
}

const TotalIterations = 50

// 1. Updated signature to return (PredictionResult, error) matches the return statements
func StartNaivePredictionCheck(log *slog.Logger, k int) error {
	config := config.LoadConfig()

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

	// We don't need to pass a pointer in; we can just get the result back
	isCorrectTotal := 0
	for i := range TotalIterations {
		log.Info(fmt.Sprintf("Naive Prediction Check Iteration: %d", i+1))
		result, err := NaivePredictionCheck(db, log, k)
		if err != nil {
			return err
		}

		log.Info(fmt.Sprintf("Final Prediction Result: %+v", result))

		if result.IsCorrect {
			isCorrectTotal++
		}
	}
	correctPercentage := (float64(isCorrectTotal) / TotalIterations) * 100.0
	log.Info(fmt.Sprintf("Overall Correct Predictions: %d out of %d (%.2f%%)", isCorrectTotal, TotalIterations, correctPercentage))
	return nil
}

// 2. Removed pointer argument 'ResultPrediction', just return the struct
func NaivePredictionCheck(db *db.Postgresql, log *slog.Logger, k int) (PredictionResult, error) {
	resultPrediction := PredictionResult{} // Initialize the result struct

	// Query 1: Get Random Row
	query_random_row := `
        select embedding, next_slope_5
        from market_pattern_go TABLESAMPLE SYSTEM(0.1)
        where close_price is not null
            and next_return  is not null
            and next_slope_3 is not null
            and next_slope_5 is not null
        order by random()
        limit 1;
    `

	rows, err := db.DB.Query(query_random_row)
	if err != nil {
		return PredictionResult{}, err
	}
	defer rows.Close()

	var answerSlope5 float64
	var embeddingVec pgvector.Vector // 3. Use pgvector type directly (no JSON needed)

	if rows.Next() {
		if err := rows.Scan(&embeddingVec, &answerSlope5); err != nil {
			log.Info(fmt.Sprintf("Scan error: %v", err))
			return PredictionResult{}, err
		}
	} else {
		return PredictionResult{}, fmt.Errorf("no rows found in random selection")
	}

	// Convert pgvector (float32) to float64 for your struct if needed,
	// but we can use embeddingVec directly for the next query.
	log.Info(fmt.Sprintf("Random Row Embedding (First 5): %v", embeddingVec.Slice()[:5]))
	log.Info(fmt.Sprintln("With next slope_5: ", answerSlope5))

	// Query 2: Find Neighbors
	sql := `
        SELECT 
            time, symbol, interval, 
            next_return, next_slope_3, next_slope_5, 
            embedding,
            (embedding <=> $1) as distance
        FROM market_pattern_go
        WHERE next_return IS NOT NULL
            AND symbol = 'ETHUSDT'
            AND interval = '15m'
        ORDER BY distance ASC
        LIMIT $2
    `

	similarRows, err := db.DB.Query(sql, embeddingVec, k)
	if err != nil {
		return PredictionResult{}, err
	}
	defer similarRows.Close()

	var results []PatternLabel

	log.Info("Fetching similar rows...")
	for similarRows.Next() {
		var r PatternLabel
		var rawTime int64
		var slope3, slope5 *float64
		var vec pgvector.Vector

		err := similarRows.Scan(
			&rawTime, &r.Symbol, &r.Interval,
			&r.NextReturn, &slope3, &slope5,
			&vec,
			&r.Distance,
		)
		if err != nil {
			return PredictionResult{}, err
		}

		r.Time = time.Unix(rawTime, 0).UTC()
		if slope3 != nil {
			r.NextSlope3 = *slope3
		}
		if slope5 != nil {
			r.NextSlope5 = *slope5
		}

		// Convert vector to slice for the struct
		r.Embedding = make([]float64, len(vec.Slice()))
		for i, v := range vec.Slice() {
			r.Embedding[i] = float64(v)
		}

		// Filter out the exact same row (distance 0)
		if r.Distance > 0 {
			results = append(results, r)
		}
	}

	// 4. Calculate Prediction (Optimized Loop)
	var positiveSlope5Count int
	var negativeSlope5Count int

	for _, res := range results {
		if res.NextSlope5 > 0 {
			positiveSlope5Count++
		} else if res.NextSlope5 < 0 {
			negativeSlope5Count++
		}
	}

	resultPrediction.PositiveCount = positiveSlope5Count
	resultPrediction.NegativeCount = negativeSlope5Count

	log.Info(fmt.Sprintf("Overall Prediction: Positive Vs Negative (%d vs %d)", positiveSlope5Count, negativeSlope5Count))

	if positiveSlope5Count == negativeSlope5Count {
		log.Info("Equal Prediction, cannot decide")
		return resultPrediction, nil
	}

	// Check correctness
	isPredictionPositive := positiveSlope5Count > negativeSlope5Count
	isAnswerPositive := answerSlope5 > 0

	// Logic: If Prediction matches Answer direction
	if (isPredictionPositive && isAnswerPositive) || (!isPredictionPositive && !isAnswerPositive) {
		log.Info("Correct Prediction")
		resultPrediction.IsCorrect = true
	} else {
		log.Info("Wrong Prediction")
		resultPrediction.IsCorrect = false
	}

	// Calculate diff
	resultPrediction.NumDiffCount = math.Abs(float64(positiveSlope5Count) - float64(negativeSlope5Count))

	// 5. Return the populated result!
	return resultPrediction, nil
}
