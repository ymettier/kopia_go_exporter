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

func TestGet_InitializesDefault(t *testing.T) {
	mu.Lock()
	global = nil
	mu.Unlock()

	l := Get()
	assert.NotNil(t, l)
	assert.Equal(t, global, l)
}

func TestGet_ReturnsExisting(t *testing.T) {
	custom := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mu.Lock()
	global = custom
	mu.Unlock()

	l := Get()
	assert.Equal(t, custom, l)
}

func TestReset_WithOptions(t *testing.T) {
	Reset(&LogOptions{
		Level:    "debug",
		JSON:     false,
		Filename: "stdout",
	})

	l := Get()
	assert.NotNil(t, l)
}

func TestReset_NilOptions(t *testing.T) {
	Reset(nil)
	l := Get()
	assert.NotNil(t, l)
}

func TestReset_JSON(t *testing.T) {
	Reset(&LogOptions{
		JSON: true,
	})
	l := Get()
	assert.NotNil(t, l)
}

func TestGetWriter_NilOptions(t *testing.T) {
	w, ok := getWriter(nil)
	assert.Equal(t, os.Stderr, w)
	assert.False(t, ok)
}

func TestGetWriter_EmptyFilename(t *testing.T) {
	w, ok := getWriter(&LogOptions{Filename: ""})
	assert.Equal(t, os.Stderr, w)
	assert.False(t, ok)
}

func TestGetWriter_Stdout(t *testing.T) {
	w, ok := getWriter(&LogOptions{Filename: "stdout"})
	assert.Equal(t, os.Stdout, w)
	assert.False(t, ok)
}

func TestGetWriter_Stderr(t *testing.T) {
	w, ok := getWriter(&LogOptions{Filename: "stderr"})
	assert.Equal(t, os.Stderr, w)
	assert.False(t, ok)
}

func TestGetWriter_Lumberjack(t *testing.T) {
	tmpFile := t.TempDir() + "/test.log"
	w, ok := getWriter(&LogOptions{
		Filename:   tmpFile,
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     7,
		Compress:   true,
	})
	require.NotNil(t, w)
	assert.True(t, ok)
}

func TestGetWriter_LumberjackDefaults(t *testing.T) {
	tmpFile := t.TempDir() + "/test_defaults.log"
	w, ok := getWriter(&LogOptions{
		Filename: tmpFile,
	})
	require.NotNil(t, w)
	assert.True(t, ok)
}

func TestNewLogger_DefaultLevel(t *testing.T) {
	Reset(nil)
	l := Get()
	require.NotNil(t, l)
	l.Info("test message at default level")
}

func TestNewLogger_DebugLevel(t *testing.T) {
	Reset(&LogOptions{Level: "debug"})
	l := Get()
	require.NotNil(t, l)
	l.Debug("test debug message")
}

func TestNewLogger_WarnLevel(t *testing.T) {
	Reset(&LogOptions{Level: "warn"})
	l := Get()
	require.NotNil(t, l)
	l.Warn("test warn message")
}

func TestNewLogger_ErrorLevel(t *testing.T) {
	Reset(&LogOptions{Level: "error"})
	l := Get()
	require.NotNil(t, l)
	l.Error("test error message")
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	Reset(&LogOptions{Level: "invalid"})
	l := Get()
	require.NotNil(t, l)
}

func TestNewLogger_EnvFallback(t *testing.T) {
	t.Setenv("KGE_LOG_LEVEL", "debug")
	Reset(&LogOptions{})
	l := Get()
	require.NotNil(t, l)
}

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

func TestReset_Concurrent(t *testing.T) {
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			Reset(&LogOptions{Level: "info"})
			_ = Get()
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
