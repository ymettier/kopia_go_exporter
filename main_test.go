// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kopia-go-exporter/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitReady blocks until run signals that its first loop
// iteration is done, or fails the test after a timeout. It replaces
// fragile time.Sleep-based synchronization.
func waitReady(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not reach its first loop iteration")
	}
}

// Checks that the embedded version string is non-empty.
func TestVersionEmbedded(t *testing.T) {
	assert.NotEmpty(t, version, "version.txt should be embedded")
}

// Runs with a nonexistent config file and expects an error mentioning
// the failure to read the configuration file.
func TestRun_MissingConfigFile(t *testing.T) {
	err := run(context.Background(), []string{"--config", "/nonexistent/config.yaml"}) //nolint:goconst
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

// Runs with --help and expects flag.ErrHelp to be returned.
func TestRun_HelpFlag(t *testing.T) {
	err := run(context.Background(), []string{"--help"})
	assert.Equal(t, flag.ErrHelp, err)
}

// Runs with each required kopia field missing and expects an error
// naming the missing field.
func TestRun_MissingRequiredConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{
			name: "password",
			cfg: `kopia:
  password: ""
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
`,
			wantErr: "kopia.password is not set",
		},
		{
			name: "repositoryURL",
			cfg: `kopia:
  password: "secret"
  apiserver:
    repositoryURL: ""
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
`,
			wantErr: "kopia.apiserver.repositoryURL is not set",
		},
		{
			name: "fingerprint",
			cfg: `kopia:
  password: "secret"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: ""
`,
			wantErr: "kopia.apiserver.fingerprint is not set",
		},
		{
			name: "hostname",
			cfg: `kopia:
  password: "secret"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: ""
    username: "kopia"
    fingerprint: "abc123"
`,
			wantErr: "kopia.apiserver.hostname is not set",
		},
		{
			name: "username",
			cfg: `kopia:
  password: "secret"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: ""
    fingerprint: "abc123"
`,
			wantErr: "kopia.apiserver.username is not set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgFile := writeTestMainConfig(t, tt.cfg)
			err := run(context.Background(), []string{"--config", cfgFile})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// Runs the main loop, cancels the context, and expects run to return
// without error after cancellation.
func TestRun_ContextCancel(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
exporter:
  port: 9091
  interval: 1
`)

	ctx, cancel := context.WithCancel(context.Background())
	testReady = make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--config", cfgFile})
	}()

	waitReady(t, testReady)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit after context cancellation")
	}
}

// Runs with KGE_LOGGER_LOG_LEVEL=debug and expects the active logger to
// enable the debug level.
func TestRun_LoggerConfigFromEnvVar(t *testing.T) {
	t.Setenv("KGE_LOGGER_LOG_LEVEL", "debug")

	cfgFile := writeTestMainConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
exporter:
  interval: 1
`)

	ctx, cancel := context.WithCancel(context.Background())
	testReady = make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--config", cfgFile})
	}()

	waitReady(t, testReady)
	l := logger.Get()
	assert.True(t, l.Enabled(context.Background(), slog.LevelDebug))
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit after context cancellation")
	}
}

// Runs with log_level=warn in the config file and expects the logger to
// enable warn and disable info.
func TestRun_LoggerConfigFromFile(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
logger:
  log_level: "warn"
exporter:
  interval: 1
`)

	ctx, cancel := context.WithCancel(context.Background())
	testReady = make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--config", cfgFile})
	}()

	waitReady(t, testReady)
	l := logger.Get()
	assert.True(t, l.Enabled(context.Background(), slog.LevelWarn))
	assert.False(t, l.Enabled(context.Background(), slog.LevelInfo))
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit after context cancellation")
	}
}

// Runs with KGE_LOGGER_JSON=true and expects the logger handler to be a
// JSON handler.
func TestRun_LoggerJSONFromEnvVar(t *testing.T) {
	t.Setenv("KGE_LOGGER_JSON", "true")

	cfgFile := writeTestMainConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
exporter:
  interval: 1
`)

	ctx, cancel := context.WithCancel(context.Background())
	testReady = make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--config", cfgFile})
	}()

	waitReady(t, testReady)
	l := logger.Get()
	_, isJSON := l.Handler().(*slog.JSONHandler)
	assert.True(t, isJSON, "logger handler should be JSONHandler when KGE_LOGGER_JSON=true")
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit after context cancellation")
	}
}

// Runs with an invalid TMPDIR and expects an error mentioning the
// failure to create the temp directory.
func TestRun_NewKopiaClientFails(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
`)
	t.Setenv("TMPDIR", "/nonexistent-kopia-tmp-dir")

	err := run(context.Background(), []string{"--config", cfgFile})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temp directory")
}

// Runs the main loop, lets it complete one iteration, then cancels, and
// expects run to exit without error.
func TestRun_LoopDecrementsInterval(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
exporter:
  port: 9092
  interval: 2
`)
	ctx, cancel := context.WithCancel(context.Background())
	testReady = make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--config", cfgFile})
	}()

	// Wait until the loop has performed its first iteration (which exercises
	// the interval reset branch), then cancel.
	waitReady(t, testReady)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit after context cancellation")
	}
}

func writeTestMainConfig(t *testing.T, content string) string {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "test.yaml")
	err := os.WriteFile(tmpFile, []byte(content), 0o600)
	require.NoError(t, err)
	return tmpFile
}
