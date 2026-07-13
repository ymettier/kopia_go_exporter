// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

type ctxKey struct{}

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

func getWriter(opts *LogOptions) (io.Writer, bool) {
	filename := ""
	if opts != nil && opts.Filename != "" {
		filename = opts.Filename
	}

	if filename == "" {
		return os.Stderr, false
	}

	switch filename {
	case "stdout":
		return os.Stdout, false
	case "stderr": //nolint:goconst
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

func newLogger(opts *LogOptions) *slog.Logger {
	level := slog.LevelInfo
	var levelStr string
	if opts != nil && opts.Level != "" {
		levelStr = opts.Level
	} else {
		levelStr = os.Getenv("KGE_LOG_LEVEL")
	}

	if levelStr != "" {
		var l slog.Level
		if err := l.UnmarshalText([]byte(strings.ToUpper(levelStr))); err != nil {
			log.Println(fmt.Errorf("invalid level, defaulting to INFO: %w", err))
		} else {
			level = l
		}
	}

	handlerOpts := &slog.HandlerOptions{
		Level: level,
	}

	w, usingLumberjack := getWriter(opts)

	var handler slog.Handler
	if opts != nil && opts.JSON {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}
	l := slog.New(handler)

	if opts != nil {
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

// Reset re-initializes the global logger with the provided options.
func Reset(opts *LogOptions) {
	mu.Lock()
	defer mu.Unlock()
	global = newLogger(opts)
}

// FromCtx returns the Logger associated with the ctx. If no logger
// is associated, the default logger is returned.
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return Get()
}

// WithCtx returns a copy of ctx with the Logger attached.
func WithCtx(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// RedactURL returns rawURL with any userinfo (username/password) stripped,
// so it can be logged without leaking credentials.
func RedactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}
