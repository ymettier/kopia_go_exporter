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

func getLogWriter(opts *LogOptions) (io.Writer, bool) {
	filename := ""
	if opts != nil && opts.Filename != "" {
		filename = opts.Filename
	}

	if filename == "" {
		return os.Stderr, false
	}

	switch filename {
	case "stdout": //nolint:goconst
		return os.Stdout, false
	case "stderr":
		return os.Stderr, false
	}

	l := &lumberjack.Logger{
		Filename: filename,
	}
	l.MaxSize = opts.MaxSize
	l.MaxBackups = opts.MaxBackups
	l.MaxAge = opts.MaxAge
	l.Compress = opts.Compress
	return l, true
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

	w, usingLumberjack := getLogWriter(opts)

	var handler slog.Handler
	if opts != nil && opts.JSON {
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	}
	l := slog.New(handler)

	logConfig(l, opts, usingLumberjack)

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
func Reset(opts *LogOptions) {
	mu.Lock()
	defer mu.Unlock()
	global = newLogger(opts)
}
