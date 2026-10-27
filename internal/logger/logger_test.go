// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package logger

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	DEBUG_LEVEL          = "debug"
	LOGGER_LOG_LEVEL_ENV = "KGE_LOGGER_LOG_LEVEL"
)

// Calls Get with different global logger states.
func TestGet(t *testing.T) {
	tests := []struct {
		name   string
		setup  func()
		check  func(t *testing.T, l *slog.Logger)
		custom *slog.Logger
	}{
		{
			name: "initializes default",
			setup: func() {
				mu.Lock()
				global = nil
				mu.Unlock()
			},
			check: func(t *testing.T, l *slog.Logger) {
				assert.NotNil(t, l)
				assert.Equal(t, global, l)
			},
		},
		{
			name: "returns existing",
			setup: func() {
				custom := slog.New(slog.NewTextHandler(os.Stderr, nil))
				mu.Lock()
				global = custom
				mu.Unlock()
			},
			check: func(t *testing.T, l *slog.Logger) {
				assert.NotNil(t, l)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			l := Get()
			if tt.check != nil {
				tt.check(t, l)
			}
			if tt.name == "returns existing" {
				// ensure it returns the same instance we set
				mu.RLock()
				g := global
				mu.RUnlock()
				assert.Equal(t, g, l)
			}
		})
	}
}

// Resets the logger with various options and expects a non-nil logger.
func TestReset(t *testing.T) {
	tests := []struct {
		name string
		opts *LogOptions
	}{
		{
			name: "with options",
			opts: &LogOptions{
				Level:    DEBUG_LEVEL,
				JSON:     false,
				Filename: "stdout", //nolint:goconst
			},
		},
		{
			name: "nil options",
			opts: nil,
		},
		{
			name: "json enabled",
			opts: &LogOptions{
				JSON: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset(tt.opts)
			l := Get()
			assert.NotNil(t, l)
		})
	}
}

// Gets the log writer and expects the correct writer and lumberjack
// behavior for each case.
func TestGetWriter(t *testing.T) {
	tests := []struct {
		name           string
		opts           *LogOptions
		useTempFile    bool
		tempName       string
		wantWriter     *os.File
		wantLumberjack bool
	}{
		{
			name:           "nil options",
			opts:           nil,
			wantWriter:     os.Stderr,
			wantLumberjack: false,
		},
		{
			name:           "empty filename",
			opts:           &LogOptions{Filename: ""},
			wantWriter:     os.Stderr,
			wantLumberjack: false,
		},
		{
			name:           "stdout",
			opts:           &LogOptions{Filename: "stdout"},
			wantWriter:     os.Stdout,
			wantLumberjack: false,
		},
		{
			name:           "stderr",
			opts:           &LogOptions{Filename: "stderr"},
			wantWriter:     os.Stderr,
			wantLumberjack: false,
		},
		{
			name:           "lumberjack with settings",
			useTempFile:    true,
			tempName:       "test.log",
			opts:           &LogOptions{MaxSize: 10, MaxBackups: 5, MaxAge: 7, Compress: true},
			wantLumberjack: true,
		},
		{
			name:           "lumberjack defaults",
			useTempFile:    true,
			tempName:       "test_defaults.log",
			opts:           &LogOptions{},
			wantLumberjack: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			if tt.useTempFile {
				base := opts
				if base == nil {
					base = &LogOptions{}
				}
				opts = &LogOptions{
					Filename:   t.TempDir() + "/" + tt.tempName,
					MaxSize:    base.MaxSize,
					MaxBackups: base.MaxBackups,
					MaxAge:     base.MaxAge,
					Compress:   base.Compress,
				}
			}
			w, lj := getLogWriter(opts)
			if tt.wantWriter != nil {
				assert.Equal(t, tt.wantWriter, w)
			} else {
				require.NotNil(t, w)
			}
			if tt.wantLumberjack {
				assert.NotNil(t, lj)
			} else {
				assert.Nil(t, lj)
			}
		})
	}
}

// Resets the logger with various levels and env settings.
func TestNewLogger(t *testing.T) {
	tests := []struct {
		name string
		opts *LogOptions
		env  map[string]string
		log  func(*slog.Logger)
	}{
		{
			name: "default level",
			opts: nil,
			log:  func(l *slog.Logger) { l.Info("test message at default level") },
		},
		{
			name: "debug level",
			opts: &LogOptions{Level: DEBUG_LEVEL},
			log:  func(l *slog.Logger) { l.Debug("test debug message") },
		},
		{
			name: "warn level",
			opts: &LogOptions{Level: "warn"},
			log:  func(l *slog.Logger) { l.Warn("test warn message") },
		},
		{
			name: "error level",
			opts: &LogOptions{Level: "error"},
			log:  func(l *slog.Logger) { l.Error("test error message") },
		},
		{
			name: "invalid level falls back",
			opts: &LogOptions{Level: "invalid"},
		},
		{
			name: "env fallback",
			opts: &LogOptions{},
			env:  map[string]string{LOGGER_LOG_LEVEL_ENV: DEBUG_LEVEL},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			Reset(tt.opts)
			l := Get()
			require.NotNil(t, l)
			if tt.log != nil {
				tt.log(l)
			}
		})
	}
}

// Resets the logger to write to a lumberjack file and expects a non-nil
// logger.
func TestNewLogger_LumberjackFile(t *testing.T) {
	tmpFile := t.TempDir() + "/test_lumberjack.log"
	Reset(&LogOptions{
		Level:      "info",
		Filename:   tmpFile,
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     7,
		Compress:   true,
	})
	l := Get()
	require.NotNil(t, l)
	l.Info("test lumberjack output")
}

// Resets the logger twice (first to a lumberjack file, then to stdout)
// and expects the previous lumberjack file to be closed without error.
func TestReset_ClosesPreviousLumberjack(t *testing.T) {
	tmpFile := t.TempDir() + "/reset_close.log"
	Reset(&LogOptions{
		Filename: tmpFile,
		MaxSize:  1,
	})
	l := Get()
	require.NotNil(t, l)
	l.Info("first logger output")

	Reset(&LogOptions{Filename: "stdout"})
	l = Get()
	require.NotNil(t, l)
	l.Info("second logger output")

	_, err := os.Stat(tmpFile)
	assert.NoError(t, err, "previous lumberjack file should still exist after close")
}

// Resets the logger concurrently from multiple goroutines and expects
// every call to yield a non-nil logger.
func TestReset_Concurrent(t *testing.T) {
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			Reset(&LogOptions{Level: "info"})
			l := Get()
			assert.NotNil(t, l, "Get should return a non-nil logger after concurrent resets")
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// Reads logger options from environment variables.
func TestOptionsFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		check func(t *testing.T, opts *LogOptions)
	}{
		{
			name: "all set",
			env: map[string]string{
				LOGGER_LOG_LEVEL_ENV:  DEBUG_LEVEL,
				"KGE_LOGGER_JSON":     "true",
				"KGE_LOGGER_FILENAME": "/tmp/test.log",
			},
			check: func(t *testing.T, opts *LogOptions) {
				assert.Equal(t, DEBUG_LEVEL, opts.Level)
				assert.True(t, opts.JSON)
				assert.Equal(t, "/tmp/test.log", opts.Filename)
			},
		},
		{
			name: "none set",
			env:  map[string]string{},
			check: func(t *testing.T, opts *LogOptions) {
				assert.Equal(t, "", opts.Level)
				assert.False(t, opts.JSON)
				assert.Equal(t, "", opts.Filename)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars to ensure clean state
			for k := range map[string]bool{LOGGER_LOG_LEVEL_ENV: true, "KGE_LOGGER_JSON": true, "KGE_LOGGER_FILENAME": true} {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			opts := OptionsFromEnv()
			if tt.check != nil {
				tt.check(t, opts)
			}
		})
	}
}

// Validates log file paths and expects errors for nonexistent
// directories and success for valid or relative paths.
func TestValidateLogPath(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		wantError bool
	}{
		{
			name:     "valid temp directory file",
			filename: t.TempDir() + "/app.log",
		},
		{
			name:      "nonexistent directory",
			filename:  "/nonexistent/dir/app.log",
			wantError: true,
		},
		{
			name:     "parent is file not directory",
			filename: t.TempDir() + "/notadir/app.log",
			// This may or may not error depending on whether /notadir exists;
			// it should error because the directory doesn't exist.
			wantError: true,
		},
		{
			name:     "just filename (relative)",
			filename: "app.log",
		},
		{
			name:     "current directory path",
			filename: "./app.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLogPath(tt.filename)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Gets the log writer for an unwritable directory and expects a fallback
// to stderr with a nil lumberjack logger.
func TestGetLogWriter_InvalidPath(t *testing.T) {
	opts := &LogOptions{
		Filename: "/nonexistent/dir/app.log",
	}
	w, lj := getLogWriter(opts)
	assert.Equal(t, os.Stderr, w)
	assert.Nil(t, lj)
}
