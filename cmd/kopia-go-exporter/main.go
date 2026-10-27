// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	_ "embed"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"kopia-go-exporter/internal/config"
	"kopia-go-exporter/internal/exporter"
	"kopia-go-exporter/internal/kopiametrics"
	"kopia-go-exporter/internal/logger"
)

//go:embed version.txt
var version string

// testReady is an optional readiness signal, set by tests, that run closes
// once the main loop has performed its first iteration. It is nil in production
// and therefore has no effect on the normal code path.
var testReady chan struct{}

func run(ctx context.Context, args []string) error {
	logger.Reset(logger.OptionsFromEnv())
	ver := strings.TrimSpace(version)
	cfg, err := config.New(ver, args)
	if err != nil {
		return err
	}
	logger.Reset(&logger.LogOptions{
		Level:      cfg.Logger.Level,
		JSON:       cfg.Logger.JSON,
		Filename:   cfg.Logger.Filename,
		MaxSize:    cfg.Logger.LogFile.MaxSize,
		MaxBackups: cfg.Logger.LogFile.MaxBackups,
		MaxAge:     cfg.Logger.LogFile.MaxAge,
		Compress:   cfg.Logger.LogFile.Compress,
	})
	l := logger.Get()
	l.Debug("Debug logging enabled")
	ex := exporter.NewExporter(cfg.Exporter, ver)
	k, err := kopiametrics.NewKopiaClientManager(cfg)
	if err != nil {
		return err
	}
	k.RegisterKopiaMetrics(ex.Reg)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go ex.Run(ctx)
	sleepInterval := 0
	for {
		select {
		case <-ctx.Done():
			k.Disconnect(context.Background()) // use fresh context: ctx is already canceled here
			return nil
		default:
			if sleepInterval == 0 {
				l.Debug("Start a new iteration of main loop...")
				if err := k.RunOnce(ctx); err != nil {
					l.Error("RunOnce failed", "err", err)
				}
				sleepInterval = cfg.Exporter.Interval
				l.Debug("Now sleeping", "Duration (sec)", cfg.Exporter.Interval)
				if testReady != nil {
					close(testReady)
					testReady = nil
				}
			} else {
				sleepInterval--
			}
			time.Sleep(time.Second)
		}
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	if err := run(ctx, os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	stop()
}
