// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionEmbedded(t *testing.T) {
	assert.NotEmpty(t, version, "version.txt should be embedded")
}

func TestRun_MissingConfigFile(t *testing.T) {
	err := run(context.Background(), []string{"--config", "/nonexistent/config.yaml"}) //nolint:goconst
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

func TestRun_HelpFlag(t *testing.T) {
	err := run(context.Background(), []string{"--help"})
	assert.Equal(t, flag.ErrHelp, err)
}

func TestRun_MissingRequiredConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{
			name: "password",
			cfg: `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
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
    repositoryURL: "https://example.com:51515"
    hostname: "localhost"
    username: "kopia"
`,
			wantErr: "kopia.apiserver.fingerprint is not set",
		},
		{
			name: "hostname",
			cfg: `kopia:
  password: "secret"
  apiserver:
    repositoryURL: "https://example.com:51515"
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
    repositoryURL: "https://example.com:51515"
    hostname: "localhost"
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

func TestRun_ContextCancel(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `exporter:
  port: 9091
  interval: 1
kopia:
  password: "secret"
  apiserver:
    repositoryURL: "https://127.0.0.1:1"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "0000000000000000000000000000000000000000000000000000000000000000"
log_level: "error"
`)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--config", cfgFile})
	}()

	time.Sleep(500 * time.Millisecond)
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
	err := os.WriteFile(tmpFile, []byte(content), 0o644)
	require.NoError(t, err)
	return tmpFile
}
