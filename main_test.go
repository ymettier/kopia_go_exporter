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
		require.FailNow(t, "run did not reach its first loop iteration")
	}
}

// Checks that the embedded version string is non-empty.
func TestVersionEmbedded(t *testing.T) {
	assert.NotEmpty(t, version, "version.txt should be embedded")
}

// Runs with simple error cases.
func TestRun_SimpleErrors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    error
		wantErrSub string
	}{
		{
			name:       "missing config file",
			args:       []string{"--config", "/nonexistent/config.yaml"},
			wantErrSub: "failed to read configuration file",
		},
		{
			name:    "help flag",
			args:    []string{"--help"},
			wantErr: flag.ErrHelp,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(context.Background(), tt.args)
			if tt.wantErr != nil {
				assert.Equal(t, tt.wantErr, err)
				return
			}
			if tt.wantErrSub != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSub)
				return
			}
			assert.NoError(t, err)
		})
	}
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
			name: "repositoryURL",
			cfg: `kopia:
  apiserver:
    repositoryURL: ""
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "secret"
`,
			wantErr: "kopia.apiserver.repositoryURL is not set",
		},
		{
			name: "fingerprint",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: ""
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "secret"
`,
			wantErr: "kopia.apiserver.fingerprint is not set",
		},
		{
			name: "no clients",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
`,
			wantErr: "kopia.clients is empty",
		},
		{
			name: "empty username",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: ""
      hostname: "localhost"
      password: "secret"
`,
			wantErr: "kopia.clients.default.username is not set",
		},
		{
			name: "empty hostname",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: ""
      password: "secret"
`,
			wantErr: "kopia.clients.default.hostname is not set",
		},
		{
			name: "empty password",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: ""
`,
			wantErr: "kopia.clients.default.password is not set",
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
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "test"
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
		require.FailNow(t, "run did not exit after context cancellation")
	}
}

// Runs logger configuration scenarios.
func TestRun_LoggerConfig(t *testing.T) {
	tests := []struct {
		name  string
		cfg   string
		env   map[string]string
		check func(t *testing.T)
	}{
		{
			name: "debug level from env",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "test"
exporter:
  interval: 1
`,
			env: map[string]string{"KGE_LOGGER_LOG_LEVEL": "debug"},
			check: func(t *testing.T) {
				l := logger.Get()
				assert.True(t, l.Enabled(context.Background(), slog.LevelDebug))
			},
		},
		{
			name: "warn level from file",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "test"
logger:
  log_level: "warn"
exporter:
  interval: 1
`,
			check: func(t *testing.T) {
				l := logger.Get()
				assert.True(t, l.Enabled(context.Background(), slog.LevelWarn))
				assert.False(t, l.Enabled(context.Background(), slog.LevelInfo))
			},
		},
		{
			name: "json from env",
			cfg: `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "test"
exporter:
  interval: 1
`,
			env: map[string]string{"KGE_LOGGER_JSON": "true"},
			check: func(t *testing.T) {
				l := logger.Get()
				_, isJSON := l.Handler().(*slog.JSONHandler)
				assert.True(t, isJSON, "logger handler should be JSONHandler when KGE_LOGGER_JSON=true")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfgFile := writeTestMainConfig(t, tt.cfg)
			ctx, cancel := context.WithCancel(context.Background())
			testReady = make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- run(ctx, []string{"--config", cfgFile})
			}()
			waitReady(t, testReady)
			if tt.check != nil {
				tt.check(t)
			}
			cancel()
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(5 * time.Second):
				require.FailNow(t, "run did not exit after context cancellation")
			}
		})
	}
}

// Runs with an invalid TMPDIR and expects an error mentioning the
// failure to create the temp directory.
func TestRun_NewKopiaClientFails(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "test"
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
  apiserver:
    repositoryURL: "http://127.0.0.1:1"
    fingerprint: "abc123"
  clients:
    default:
      username: "kopia"
      hostname: "localhost"
      password: "test"
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
		require.FailNow(t, "run did not exit after context cancellation")
	}
}

func writeTestMainConfig(t *testing.T, content string) string {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "test.yaml")
	err := os.WriteFile(tmpFile, []byte(content), 0o600)
	require.NoError(t, err)
	return tmpFile
}
