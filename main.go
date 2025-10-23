package main

import (
	"context"
	_ "embed"
	"kopia-go-exporter/exporter"
	"kopia-go-exporter/kopiametrics"
	"kopia-go-exporter/modconfig"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed version.txt
var version string

func main() {
	modconfig.LoadConfig(version)

	logger := new_logger()
	logger.Debug().Msg("Debug logging enabled")

	exporter.Logger = logger
	ex := exporter.NewExporter()

	k := kopiametrics.NewKopiaClient(&ex.Metrics)
	kopiametrics.Logger = logger

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info().Msg("Caught interrupt signal")
		cancel()
	}()

	go ex.Run()

	for {
		select {
		case <-ctx.Done():
			k.Disconnect()
			return
		default:
			logger.Debug().Msg("Start a new iteration of main loop...")
			k.RunOnce()
			logger.Debug().Int("Duration (sec)", modconfig.Cfg.Exporter.Interval).Msg("Now sleeping")
			time.Sleep(time.Duration(modconfig.Cfg.Exporter.Interval) * time.Second)
		}
	}
}
