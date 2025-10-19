package main

import (
	"os"

	"github.com/rs/zerolog"
)

func new_logger() zerolog.Logger {
	// Set up zerolog logging level
	level, err := zerolog.ParseLevel(Cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02T15:04:05Z07:00"}
	logger := zerolog.New(consoleWriter).With().Timestamp().Logger()

	logger.Info().
		Str("server", Cfg.Server.Name).
		Int("port", Cfg.Server.Port).
		Msg("Server starting with YAML config")

	return logger
}
