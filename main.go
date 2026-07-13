// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	_ "embed"
	"flag"
	"kopia-go-exporter/config"
	"kopia-go-exporter/exporter"
	"kopia-go-exporter/kopiametrics"
	"kopia-go-exporter/logger"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed version.txt
var version string

func main() {
	if err := config.New(version, os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}
	if err := config.CheckConfig(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	logger.Reset(&logger.LogOptions{
		Level: config.Cfg.LogLevel,
	})
	l := logger.Get()
	l.Debug("Debug logging enabled")

	exporter.Logger = l
	ex := exporter.NewExporter()

	k := kopiametrics.NewKopiaClient()
	kopiametrics.Logger = l
	k.RegisterKopiaMetrics(ex.Reg)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		l.Info("Caught interrupt signal")
		cancel()
	}()

	go ex.Run()

	sleepInterval := 0
	for {
		select {
		case <-ctx.Done():
			k.Disconnect()
			return
		default:
			if sleepInterval == 0 {
				l.Debug("Start a new iteration of main loop...")
				k.RunOnce()
				sleepInterval = config.Cfg.Exporter.Interval
				l.Debug("Now sleeping", "Duration (sec)", config.Cfg.Exporter.Interval)
			} else {
				sleepInterval--
			}
			time.Sleep(time.Second)
		}
	}
}
