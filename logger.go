package main

import (
	"kopia-go-exporter/modconfig"
	"os"

	"github.com/rs/zerolog"
)

func new_logger() zerolog.Logger {
	// Set up zerolog logging level
	level, err := zerolog.ParseLevel(modconfig.Cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02T15:04:05Z07:00"}
	logger := zerolog.New(consoleWriter).With().Timestamp().Logger()

	logger.Info().
		Int("port", modconfig.Cfg.Exporter.Port).
		Msg("Kopia exporter starting with YAML modconfig")
	return logger
}
