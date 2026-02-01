package backfill

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
	"vector-quant-monitor/internal/config"
	"vector-quant-monitor/internal/db"
)

// --- CONFIGURATION ---
const (
	BaseURL = "https://fapi.binance.com"
	Symbol  = "ETHUSDT"
)

// Raw Trade from Binance
type Trade struct {
	Id              int64  `json:"id"`
	OrderId         int64  `json:"orderId"`
	Symbol          string `json:"symbol"`
	Side            string `json:"side"`
	PositionSide    string `json:"positionSide"`
	Qty             string `json:"qty"`
	Price           string `json:"price"`
	RealizedPnl     string `json:"realizedPnl"`
	Commission      string `json:"commission"`      // Fee amount
	CommissionAsset string `json:"commissionAsset"` // e.g., USDT or BNB
	Time            int64  `json:"time"`
}

// Aggregated Order (Combines multiple fills of the same order)
type OrderGroup struct {
	OrderId         int64
	Symbol          string
	Side            string
	PositionSide    string
	TotalQty        float64
	AvgPrice        float64 // Weighted average
	TotalPnl        float64
	TotalCommission float64
	CommissionAsset string
	Time            int64
	IsClose         bool // True if RealizedPnl != 0
}

// Final Output Row
type PositionRow struct {
	Symbol       string
	Side         string
	PositionSide string
	NetPnl       string // PnL - Commission
	Vol          string
	OpenTime     time.Time
	CloseTime    time.Time
}

func GetPositionHistory(db *db.Postgresql, LookBackDays int) []PositionRow {

	// 1. Fetch raw trades
	rawTrades, err := fetchTradesWithTimeWindow(Symbol, LookBackDays)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	// 2. Group trades by OrderID to handle split fills
	orderGroups := groupTradesByOrder(rawTrades)

	// 3. Sort groups chronologically to track history
	sort.Slice(orderGroups, func(i, j int) bool {
		return orderGroups[i].Time < orderGroups[j].Time
	})

	// 4. Match Open Times with Close Times
	// Map to store the Open Time of the current position side
	activePositions := make(map[string]int64)
	var history []PositionRow

	for _, order := range orderGroups {
		key := order.PositionSide // "LONG", "SHORT", or "BOTH"

		// LOGIC:
		// If PnL is 0, it's an OPEN (or add) order.
		// We only record the time if we don't already have an open start time.
		if !order.IsClose {
			if _, exists := activePositions[key]; !exists {
				activePositions[key] = order.Time
			}
		}

		// If PnL != 0, it's a CLOSE order.
		if order.IsClose {
			openTimeVal, exists := activePositions[key]

			// Calculate Net PnL (Subtract commission if it's in USDT)
			// If Commission is in BNB, the UI usually converts it, but here we just show raw math for USDT.
			netPnl := order.TotalPnl
			if order.CommissionAsset == "USDT" {
				netPnl = order.TotalPnl - order.TotalCommission
			}

			var tOpen time.Time
			if exists {
				tOpen = time.UnixMilli(openTimeVal) // Converts 17000... to a Time object
			} else {
				// Handle edge case where open time is missing (maybe set to close time or epoch)
				tOpen = time.UnixMilli(order.Time)
			}

			tClose := time.UnixMilli(order.Time)

			row := PositionRow{
				Symbol:       order.Symbol,
				Side:         order.Side,
				PositionSide: order.PositionSide, // usually "BOTH" for One-Way mode
				NetPnl:       fmt.Sprintf("%.2f", netPnl),
				Vol:          fmt.Sprintf("%.3f", order.TotalQty),
				OpenTime:     tOpen,
				CloseTime:    tClose,
			}
			history = append(history, row)

			// Logic: If we closed, we assume position is cleared or we wait for next Open.
			// Note: This simple logic assumes full closes. Partial closes are complex to track perfectly without balance state.
			// For this specific list, deleting the key resets the timer for the next trade.
			delete(activePositions, key)
		}
	}

	// 5. Print Output (Newest First)
	fmt.Println("-----------------------------------------------------------------------------------------")
	fmt.Printf("%-10s | %-6s | %-12s | %-10s | %-20s | %-20s\n",
		"Symbol", "Side", "Net PnL", "Vol(ETH)", "Opened Time", "Closed Time")
	fmt.Println("-----------------------------------------------------------------------------------------")

	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		fmt.Printf("%-10s | %-6s | %-12s | %-10s | %-20s | %-20s\n",
			h.Symbol, h.Side, h.NetPnl, h.Vol, h.OpenTime, h.CloseTime)
	}
	fmt.Println("-----------------------------------------------------------------------------------------")

	return history
}

// --- HELPERS ---

// Aggegates individual fills into single Order events
func groupTradesByOrder(trades []Trade) []*OrderGroup {
	grouped := make(map[int64]*OrderGroup)
	var orderIds []int64 // to keep order for stable iteration if needed

	for _, t := range trades {
		qty, _ := strconv.ParseFloat(t.Qty, 64)
		price, _ := strconv.ParseFloat(t.Price, 64)
		pnl, _ := strconv.ParseFloat(t.RealizedPnl, 64)
		comm, _ := strconv.ParseFloat(t.Commission, 64)

		if grp, exists := grouped[t.OrderId]; exists {
			// Weighted Average Price Calculation
			totalVal := (grp.AvgPrice * grp.TotalQty) + (price * qty)
			grp.TotalQty += qty
			grp.AvgPrice = totalVal / grp.TotalQty
			grp.TotalPnl += pnl
			grp.TotalCommission += comm
			// Update time to latest fill time
			if t.Time > grp.Time {
				grp.Time = t.Time
			}
		} else {
			isClose := pnl != 0
			// Create new group
			newGrp := &OrderGroup{
				OrderId:         t.OrderId,
				Symbol:          t.Symbol,
				Side:            t.Side,
				PositionSide:    t.PositionSide,
				TotalQty:        qty,
				AvgPrice:        price,
				TotalPnl:        pnl,
				TotalCommission: comm,
				CommissionAsset: t.CommissionAsset,
				Time:            t.Time,
				IsClose:         isClose,
			}
			grouped[t.OrderId] = newGrp
			orderIds = append(orderIds, t.OrderId)
		}
	}

	// Convert map to slice
	var result []*OrderGroup
	for _, id := range orderIds {
		result = append(result, grouped[id])
	}
	return result
}

func fetchTradesWithTimeWindow(symbol string, hoursBack int) ([]Trade, error) {
	config := config.LoadConfig()
	ApiKey := config.Binance.ApiKey
	SecretKey := config.Binance.ApiSecret

	client := &http.Client{Timeout: 10 * time.Second}
	var allTrades []Trade

	// 1. Calculate the final end time (Now)
	endTime := time.Now().UnixMilli()
	// 2. Calculate the starting point (e.g., 20 days ago)
	startTime := time.Now().Add(time.Duration(-hoursBack) * time.Hour).UnixMilli()

	// 3. Loop in 7-day (604800000 ms) chunks
	// We use slightly less (6 days) to be safe and avoid edge case overlaps
	const ChunkSize = 6 * 24 * 60 * 60 * 1000

	for currentStart := startTime; currentStart < endTime; {
		// Calculate current chunk end
		currentEnd := currentStart + ChunkSize
		if currentEnd > endTime {
			currentEnd = endTime
		}

		fmt.Printf("Fetching chunk: %s -> %s\n",
			time.UnixMilli(currentStart).Format("2006-01-02 15:04"),
			time.UnixMilli(currentEnd).Format("2006-01-02 15:04"),
		)

		// --- API REQUEST ---
		params := url.Values{}
		params.Add("symbol", symbol)
		params.Add("limit", "1000")
		params.Add("startTime", strconv.FormatInt(currentStart, 10))
		params.Add("endTime", strconv.FormatInt(currentEnd, 10))
		params.Add("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

		queryStr := params.Encode()
		signature := computeHmac256(queryStr, SecretKey)
		fullURL := fmt.Sprintf("%s/fapi/v1/userTrades?%s&signature=%s", BaseURL, queryStr, signature)

		req, _ := http.NewRequest("GET", fullURL, nil)
		req.Header.Set("X-MBX-APIKEY", ApiKey)
		resp, err := client.Do(req)

		if err != nil {
			fmt.Println("Network error:", err)
			return nil, err
		}

		// Handle non-200 errors (like if we hit rate limits)
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("API Error %d: %s\n", resp.StatusCode, string(body))
			resp.Body.Close()
			return nil, fmt.Errorf("API Error")
		}

		var chunk []Trade
		json.NewDecoder(resp.Body).Decode(&chunk)
		resp.Body.Close()

		// Append results
		if len(chunk) > 0 {
			allTrades = append(allTrades, chunk...)
		}

		// Move valid time forward
		currentStart = currentEnd + 1

		// Sleep briefly to respect API rate limits
		time.Sleep(100 * time.Millisecond)
	}

	return allTrades, nil
}

func fetchTrades(symbol string, limit int) ([]Trade, error) {
	config := config.LoadConfig()
	ApiKey := config.Binance.ApiKey
	SecretKey := config.Binance.ApiSecret

	client := &http.Client{Timeout: 10 * time.Second}
	endpoint := "/fapi/v1/userTrades"
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("limit", strconv.Itoa(limit))
	params.Add("timestamp", strconv.FormatInt(time.Now().UnixNano()/1e6, 10))

	queryStr := params.Encode()
	signature := computeHmac256(queryStr, SecretKey)
	fullURL := fmt.Sprintf("%s%s?%s&signature=%s", BaseURL, endpoint, queryStr, signature)

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", ApiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Status %d", resp.StatusCode)
	}

	var trades []Trade
	if err := json.NewDecoder(resp.Body).Decode(&trades); err != nil {
		return nil, err
	}
	return trades, nil
}

func computeHmac256(message string, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

func formatTime(ms int64) string {
	return time.UnixMilli(ms).Format("2006-01-02 15:04:05")
}
