// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package logger

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	mu     sync.RWMutex
	global *slog.Logger
	// writer is the underlying lumberjack writer of the global logger,
	// if file rotation is enabled. It is kept so Reset can close it.
	writer *lumberjack.Logger
)

type LogOptions struct {
	JSON       bool
	Level      string
	Filename   string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

func getLogWriter(opts *LogOptions) (io.Writer, *lumberjack.Logger) {
	filename := ""
	if opts != nil && opts.Filename != "" {
		filename = opts.Filename
	}

	if filename == "" {
		return os.Stderr, nil
	}

	switch filename {
	case "stdout": //nolint:goconst
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	}

	l := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    opts.MaxSize,
		MaxBackups: opts.MaxBackups,
		MaxAge:     opts.MaxAge,
		Compress:   opts.Compress,
	}
	return l, l
}

func parseLogLevel(opts *LogOptions) slog.Level {
	level := slog.LevelInfo
	var levelStr string
	if opts != nil && opts.Level != "" {
		levelStr = opts.Level
	} else {
		levelStr = os.Getenv("KGE_LOGGER_LOG_LEVEL")
	}
	if levelStr != "" {
		var l slog.Level
		if err := l.UnmarshalText([]byte(strings.ToUpper(levelStr))); err != nil {
			log.Println(fmt.Errorf("invalid level, defaulting to INFO: %w", err))
		} else {
			level = l
		}
	}
	return level
}

func logConfig(l *slog.Logger, opts *LogOptions, usingLumberjack bool) {
	if opts == nil {
		return
	}
	attrs := []any{
		slog.String("level", opts.Level),
		slog.String("filename", opts.Filename),
		slog.Bool("json", opts.JSON),
	}
	if usingLumberjack {
		attrs = append(attrs,
			slog.Int("maxSize", opts.MaxSize),
			slog.Int("maxBackups", opts.MaxBackups),
			slog.Int("maxAge", opts.MaxAge),
			slog.Bool("compress", opts.Compress),
		)
	}
	l.Info("Logger configuration", attrs...)
}

func newLogger(opts *LogOptions) *slog.Logger {
	level := parseLogLevel(opts)

	w, lj := getLogWriter(opts)
	writer = lj

	var handler slog.Handler
	if opts != nil && opts.JSON {
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	}
	l := slog.New(handler)

	logConfig(l, opts, lj != nil)

	return l
}

// Get returns the global logger, initializing it with defaults if needed.
func Get() *slog.Logger {
	mu.RLock()
	l := global
	mu.RUnlock()

	if l != nil {
		return l
	}

	mu.Lock()
	defer mu.Unlock()
	if global == nil {
		global = newLogger(nil)
	}
	return global
}

// OptionsFromEnv returns LogOptions populated from logger-related
// environment variables. Missing variables keep their zero values,
// which lets callers fall back to defaults later.
func OptionsFromEnv() *LogOptions {
	opts := &LogOptions{}
	if v := os.Getenv("KGE_LOGGER_LOG_LEVEL"); v != "" {
		opts.Level = v
	}
	if v := os.Getenv("KGE_LOGGER_JSON"); v != "" {
		opts.JSON = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("KGE_LOGGER_FILENAME"); v != "" {
		opts.Filename = v
	}
	return opts
}

// Reset re-initializes the global logger with the provided options.
// It closes the previous lumberjack file handle if one was in use to
// avoid leaking the underlying file descriptor.
func Reset(opts *LogOptions) {
	mu.Lock()
	defer mu.Unlock()
	if writer != nil {
		_ = writer.Close()
		writer = nil
	}
	global = newLogger(opts)
}
