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

	"kopia-go-exporter/config"
)

func TestVersionEmbedded(t *testing.T) {
	assert.NotEmpty(t, version, "version.txt should be embedded")
}

func TestRun_MissingConfigFile(t *testing.T) {
	err := run(context.Background(), []string{"--config", "/nonexistent/config.yaml"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

func TestRun_HelpFlag(t *testing.T) {
	err := run(context.Background(), []string{"--help"})
	assert.Equal(t, flag.ErrHelp, err)
}

func TestRun_MissingPassword(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
`)
	err := run(context.Background(), []string{"--config", cfgFile})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.password is not set")
}

func TestRun_MissingRepositoryURL(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "secret"
  apiserver:
    hostname: "localhost"
    username: "kopia"
    fingerprint: "abc123"
`)
	err := run(context.Background(), []string{"--config", cfgFile})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.repositoryURL is not set")
}

func TestRun_MissingFingerprint(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "secret"
  apiserver:
    repositoryURL: "https://example.com:51515"
    hostname: "localhost"
    username: "kopia"
`)
	err := run(context.Background(), []string{"--config", cfgFile})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.fingerprint is not set")
}

func TestRun_MissingHostname(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "secret"
  apiserver:
    repositoryURL: "https://example.com:51515"
    username: "kopia"
    fingerprint: "abc123"
`)
	err := run(context.Background(), []string{"--config", cfgFile})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.hostname is not set")
}

func TestRun_MissingUsername(t *testing.T) {
	cfgFile := writeTestMainConfig(t, `kopia:
  password: "secret"
  apiserver:
    repositoryURL: "https://example.com:51515"
    hostname: "localhost"
    fingerprint: "abc123"
`)
	err := run(context.Background(), []string{"--config", cfgFile})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.username is not set")
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

	origCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = origCfg })

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
