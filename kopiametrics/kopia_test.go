// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/kopia/kopia/repo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func hashSHA256(pemContent []byte) (string, error) {
	block, _ := pem.Decode(pemContent)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM contents")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate block: %w", err)
	}

	fingerprint := sha256.Sum256(cert.Raw)
	return fmt.Sprintf("%x", fingerprint), nil
}

func setupTestKopia(t *testing.T) (cleanup func(), fingerprint, ip string, port nat.Port) {
	t.Helper()
	ctx := context.Background()

	kopiaRunScript := `#!/bin/bash
set -e

rm -rf /tmp/repo /tmp/cache

kopia repository create filesystem --path="/tmp/repo" -c -p kopiapwd \
  --cache-directory="/tmp/cache" --no-check-for-updates \
  --override-hostname=localhost --override-username=kopia

kopia --config-file="/tmp/repo.config" repository connect filesystem \
  --path="/tmp/repo" -p kopiapwd --cache-directory="/tmp/cache" --no-check-for-updates

kopia --config-file="/tmp/repo.config" snapshot create -p kopiapwd $(pwd) \
  --start-time="2025-05-01 15:20:01 CET" --end-time="2025-05-01 16:10:02 CET"

kopia --config-file="/tmp/repo.config" server user add kopia@localhost \
  --user-password=kopiapwd -p kopiapwd

kopia --config-file="/tmp/repo.config" server start -p kopiapwd \
  --address="http://0.0.0.0:51515" \
  --file-log-level=debug \
  --server-username=kopia \
  --server-password=kopiapwd \
  --server-control-username=kopia \
  --server-control-password=Kopia \
  --tls-generate-cert \
  --tls-cert-file "/tmp/my.cert" \
  --tls-key-file "/tmp/my.key"
`

	nw, err := network.New(ctx)
	require.NoError(t, err, "Failed to create new network")

	ctr, err := testcontainers.Run(
		ctx,
		"kopia/kopia:latest",
		network.WithNetwork([]string{"kopia"}, nw),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(kopiaRunScript),
			ContainerFilePath: "/tmp/run.sh",
			FileMode:          0o755,
		}),
		testcontainers.WithTmpfs(map[string]string{
			"/tmp": "rw",
		}),
		testcontainers.WithExposedPorts("51515/tcp"),
		testcontainers.WithEntrypoint("/usr/bin/bash"),
		testcontainers.WithCmd("/tmp/run.sh"),
		testcontainers.WithWaitStrategy(wait.ForAll(
			wait.ForListeningPort("51515/tcp").WithStartupTimeout(30*time.Second),
			wait.ForFile("/tmp/my.cert").WithPollInterval(500*time.Millisecond).WithStartupTimeout(5*time.Second),
		)),
	)
	if err != nil {
		assert.NoError(t, nw.Remove(ctx), "Failed to remove network")
		t.Fatalf("Failed to start Kopia server: %v", err)
	}

	cleanup = func() {
		ctr.Exec(ctx, []string{"bash", "-c", fmt.Sprintf(
			"kopia server shutdown --server-cert-fingerprint=%s --address=https://%s:%s --server-control-username=kopia --server-control-password=Kopia",
			fingerprint, ip, port.Port(),
		)})
		ctr.Terminate(ctx)
		assert.NoError(t, nw.Remove(ctx), "Failed to remove network")
	}

	certFile, err := ctr.CopyFileFromContainer(ctx, "/tmp/my.cert")
	require.NoError(t, err, "Failed to read /tmp/my.cert from container")

	certContents, err := io.ReadAll(certFile)
	require.NoError(t, err, "Failed to read certificate contents")

	fingerprint, err = hashSHA256(certContents)
	require.NoError(t, err, "Failed to extract fingerprint from certificate")

	ip, err = ctr.Host(ctx)
	require.NoError(t, err, "Failed to extract host ip")

	port, err = ctr.MappedPort(ctx, "51515/tcp")
	require.NoError(t, err, "Failed to extract port")

	return cleanup, fingerprint, ip, port
}

func TestNewKopiaClient(t *testing.T) {
	k := NewKopiaClient()
	assert.NotNil(t, k)
	assert.False(t, k.IsConnected)
}

func TestKopiaClient_RegisterKopiaMetrics(t *testing.T) {
	metricNames := []string{
		"total_size",
		"file_count",
		"dir_count",
		"error_count",
		"backup_duration",
		"backup_start_time",
		"backup_end_time",
	}

	k := NewKopiaClient()
	reg := prometheus.NewRegistry()
	k.RegisterKopiaMetrics(reg)

	for _, mn := range metricNames {
		collector := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: mn,
			Help: "whatever",
		})
		err := reg.Register(collector)
		assert.Error(t, err, "Expected metric '%s' not found in registry", mn)
	}
}

func TestKopiaClient_Connect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cleanup, fingerprint, ip, port := setupTestKopia(t)
	defer cleanup()

	configFile := filepath.Join(t.TempDir(), "repo.config")

	Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

	k := &KopiaClient{
		Ctx:         context.Background(),
		IsConnected: false,
		Opts: repo.ConnectOptions{
			ClientOptions: repo.ClientOptions{
				Username: "kopia",
				Hostname: ip,
			},
		},
		ServerInfo: repo.APIServerInfo{
			BaseURL:                             fmt.Sprintf("https://%s:%s", ip, port.Port()),
			TrustedServerCertificateFingerprint: fingerprint,
		},
	}

	k.Ctx = context.Background()
	opts := repo.ConnectOptions{
		ClientOptions: repo.ClientOptions{
			Username: "kopia",
			Hostname: "localhost",
		},
	}
	serverInfo := repo.APIServerInfo{
		BaseURL:                             fmt.Sprintf("https://%s:%s", ip, port.Port()),
		TrustedServerCertificateFingerprint: fingerprint,
	}

	err := repo.ConnectAPIServer(k.Ctx, configFile, &serverInfo, "kopiapwd", &opts)
	require.NoError(t, err, "Failed to connect API server")

	k.Repo, err = repo.Open(k.Ctx, configFile, "kopiapwd", nil)
	require.NoError(t, err, "Failed to open repository")
	k.IsConnected = true

	assert.True(t, k.IsConnected)
}

func TestKopiaClient_Disconnect(t *testing.T) {
	Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

	k := NewKopiaClient()
	assert.False(t, k.IsConnected)

	k.IsConnected = true
	k.Disconnect()
	assert.False(t, k.IsConnected)
}
