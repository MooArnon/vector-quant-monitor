package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
)

func StartFuturesUserStream(apiKey, secretKey string, log *slog.Logger) {
	// 1. Initialize Client to get the ListenKey
	client := binance.NewFuturesClient(apiKey, secretKey)

	// 2. Generate the initial ListenKey
	// This acts like a "session ID" for the WebSocket
	listenKey, err := client.NewStartUserStreamService().Do(context.TODO())
	if err != nil {
		log.Info(fmt.Sprintf("Error getting ListenKey: %v", err))
		return
	}

	// 3. Launch "Keep-Alive" Manager (The Heartbeat)
	// Runs in the background to prevent disconnection every 60 mins
	go func() {
		ticker := time.NewTicker(50 * time.Minute)
		for range ticker.C {
			err := client.NewKeepaliveUserStreamService().ListenKey(listenKey).Do(context.TODO())
			if err != nil {
				log.Info(fmt.Sprintf("Keep-alive failed: %v", err))
			} else {
				log.Info(fmt.Sprintln("--- 🔄 ListenKey Refreshed ---"))
			}
		}
	}()

	// 4. Define the Handler (The Logic)
	wsHandler := func(event *futures.WsUserDataEvent) {
		// Handle Futures-specific events
		switch event.Event {

		// A. Position & PnL Updates (The "Risk Monitor")
		case futures.UserDataEventTypeAccountUpdate:
			for _, pos := range event.AccountUpdate.Positions {
				log.Info(fmt.Sprintf("[Position] %s | PnL: %s\n", pos.Symbol, pos.UnrealizedPnL))
			}

		// B. Trade Updates (The "Fees & Fills")
		case futures.UserDataEventTypeOrderTradeUpdate:
			order := event.OrderTradeUpdate
			if order.Status == "FILLED" {
				log.Info(fmt.Sprintf("[Trade] %s Filled | Fee: %s %s\n",
					order.Symbol, order.Commission, order.CommissionAsset))
			}
		}
	}

	errHandler := func(err error) {
		log.Info(fmt.Sprintf("Connection Error: %v", err))
	}

	// 5. Connect
	// Note: We pass the 'listenKey' we generated in step 2
	doneC, stopC, err := futures.WsUserDataServe(listenKey, wsHandler, errHandler)
	if err != nil {
		log.Info(fmt.Sprintf("Could not connect: %v", err))
		return
	}

	// Block forever (or until disconnected)
	<-doneC
	close(stopC)
}
