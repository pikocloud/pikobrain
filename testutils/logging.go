package testutils

import (
	"log/slog"
	"os"
)

func SetupLogging() {
	lvl := new(slog.LevelVar)
	lvl.Set(slog.LevelDebug)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	}))
	slog.SetDefault(logger)
}
