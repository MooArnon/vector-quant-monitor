package util

import (
	"log/slog"
	"os"
)

func NewLogger(level string, logger_group string) *slog.Logger {
	handler := slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: slog.LevelInfo,
		},
	)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger

}
