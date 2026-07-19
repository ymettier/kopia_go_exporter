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

	"kopia-go-exporter/config"
	"kopia-go-exporter/exporter"
	"kopia-go-exporter/kopiametrics"
	"kopia-go-exporter/logger"
)

//go:embed version.txt
var version string

//go:embed config.default.yaml
var defaultConfig []byte

func run(ctx context.Context, args []string) error {
	logger.Reset(logger.OptionsFromEnv())
	if err := config.New(strings.TrimSpace(version), args, defaultConfig); err != nil {
		return err
	}
	logger.Reset(&logger.LogOptions{
		Level:      config.Cfg.Logger.Level,
		JSON:       config.Cfg.Logger.JSON,
		Filename:   config.Cfg.Logger.Filename,
		MaxSize:    config.Cfg.Logger.MaxSize,
		MaxBackups: config.Cfg.Logger.MaxBackups,
		MaxAge:     config.Cfg.Logger.MaxAge,
		Compress:   config.Cfg.Logger.Compress,
	})
	l := logger.Get()
	l.Debug("Debug logging enabled")
	ex := exporter.NewExporter(config.Cfg.Exporter)
	k, err := kopiametrics.NewKopiaClient(&config.Cfg)
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
			k.Disconnect(ctx)
			return nil
		default:
			if sleepInterval == 0 {
				l.Debug("Start a new iteration of main loop...")
				if err := k.RunOnce(ctx); err != nil {
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

	if err := run(ctx, os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	stop()
}
