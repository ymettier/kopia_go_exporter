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
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed version.txt
var version string

func run(ctx context.Context, args []string) error {
	if err := config.New(version, args); err != nil {
		return err
	}

	if err := config.CheckConfig(); err != nil {
		return err
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
			return nil
		default:
			if sleepInterval == 0 {
				l.Debug("Start a new iteration of main loop...")
				if err := k.RunOnce(); err != nil {
					l.Error("RunOnce failed", "err", err)
				}
				sleepInterval = config.Cfg.Exporter.Interval
				l.Debug("Now sleeping", "Duration (sec)", config.Cfg.Exporter.Interval)
			} else {
				sleepInterval--
			}
			time.Sleep(time.Second)
		}
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}
}
