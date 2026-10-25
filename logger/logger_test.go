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

// Calls Get with no global logger set and expects a default logger to be
// created and returned.
func TestGet_InitializesDefault(t *testing.T) {
	mu.Lock()
	global = nil
	mu.Unlock()

	l := Get()
	assert.NotNil(t, l)
	assert.Equal(t, global, l)
}

// Calls Get when a global logger is already set and expects the existing
// logger to be returned.
func TestGet_ReturnsExisting(t *testing.T) {
	custom := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mu.Lock()
	global = custom
	mu.Unlock()

	l := Get()
	assert.Equal(t, custom, l)
}

// Resets the logger with explicit options and expects a non-nil logger.
func TestReset_WithOptions(t *testing.T) {
	Reset(&LogOptions{
		Level:    "debug",
		JSON:     false,
		Filename: "stdout", //nolint:goconst
	})

	l := Get()
	assert.NotNil(t, l)
}

// Resets the logger with nil options and expects a non-nil logger.
func TestReset_NilOptions(t *testing.T) {
	Reset(nil)
	l := Get()
	assert.NotNil(t, l)
}

// Resets the logger with JSON enabled and expects a non-nil logger.
func TestReset_JSON(t *testing.T) {
	Reset(&LogOptions{
		JSON: true,
	})
	l := Get()
	assert.NotNil(t, l)
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
				opts = tt.opts
				if opts == nil {
					opts = &LogOptions{}
				}
				opts = &LogOptions{
					Filename:   t.TempDir() + "/" + tt.tempName,
					MaxSize:    tt.opts.MaxSize,
					MaxBackups: tt.opts.MaxBackups,
					MaxAge:     tt.opts.MaxAge,
					Compress:   tt.opts.Compress,
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

// Resets the logger with no level set and expects a non-nil logger at
// the default level.
func TestNewLogger_DefaultLevel(t *testing.T) {
	Reset(nil)
	l := Get()
	require.NotNil(t, l)
	l.Info("test message at default level")
}

// Resets the logger at debug level and expects a non-nil logger.
func TestNewLogger_DebugLevel(t *testing.T) {
	Reset(&LogOptions{Level: "debug"})
	l := Get()
	require.NotNil(t, l)
	l.Debug("test debug message")
}

// Resets the logger at warn level and expects a non-nil logger.
func TestNewLogger_WarnLevel(t *testing.T) {
	Reset(&LogOptions{Level: "warn"})
	l := Get()
	require.NotNil(t, l)
	l.Warn("test warn message")
}

// Resets the logger at error level and expects a non-nil logger.
func TestNewLogger_ErrorLevel(t *testing.T) {
	Reset(&LogOptions{Level: "error"})
	l := Get()
	require.NotNil(t, l)
	l.Error("test error message")
}

// Resets the logger with an invalid level and expects a non-nil logger
// that falls back to INFO.
func TestNewLogger_InvalidLevel(t *testing.T) {
	Reset(&LogOptions{Level: "invalid"})
	l := Get()
	require.NotNil(t, l)
}

// Resets the logger with KGE_LOGGER_LOG_LEVEL set and expects the level
// to come from the environment.
func TestNewLogger_EnvFallback(t *testing.T) {
	t.Setenv("KGE_LOGGER_LOG_LEVEL", "debug")
	Reset(&LogOptions{})
	l := Get()
	require.NotNil(t, l)
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

// Reads logger options from env vars all set and expects Level, JSON,
// and Filename to match.
func TestOptionsFromEnv_AllSet(t *testing.T) {
	t.Setenv("KGE_LOGGER_LOG_LEVEL", "debug")
	t.Setenv("KGE_LOGGER_JSON", "true")
	t.Setenv("KGE_LOGGER_FILENAME", "/tmp/test.log")

	opts := OptionsFromEnv()
	assert.Equal(t, "debug", opts.Level)
	assert.True(t, opts.JSON)
	assert.Equal(t, "/tmp/test.log", opts.Filename)
}

// Reads logger options from env with no vars set and expects empty
// Level/JSON/Filename defaults.
func TestOptionsFromEnv_NoneSet(t *testing.T) {
	opts := OptionsFromEnv()
	assert.Equal(t, "", opts.Level)
	assert.False(t, opts.JSON)
	assert.Equal(t, "", opts.Filename)
}
