// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kopia/kopia/repo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// setupTestKopia starts a real Kopia API server on the local machine using the
// downloaded kopia test binary (see kopia_tests_helpers_test.go). It creates a
// filesystem repository, takes a snapshot, adds a server user, and starts the
// server listening on 127.0.0.1:51515 with a freshly generated TLS certificate.
// It returns a cleanup function that shuts the server down, together with the
// server certificate fingerprint, the bind IP and the listening port.
//
// No container runtime (Docker/testcontainers) is used; the server runs as a
// local subprocess so the tests can run anywhere the binary can be executed.
func setupTestKopia(t *testing.T) (cleanup func(), fingerprint, ip, port string) {
	t.Helper()
	ctx := context.Background()

	bin := KopiaTestBinaryPath()
	if _, err := os.Stat(bin); err != nil {
		require.NoError(t, downloadKopiaBinary(t), "failed to obtain kopia test binary")
	}

	bin, err := filepath.Abs(bin)
	require.NoError(t, err, "failed to resolve kopia binary path")

	baseDir := t.TempDir()
	repoPath := filepath.Join(baseDir, "repo")
	cachePath := filepath.Join(baseDir, "cache")
	configFile := filepath.Join(baseDir, "repo.config")
	certFile := filepath.Join(baseDir, "my.cert")
	keyFile := filepath.Join(baseDir, "my.key")

	port = "51515"
	ip = "127.0.0.1"

	runKopia := func(name string, args ...string) {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = baseDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s failed: %s", name, string(out))
	}

	runKopia("repository create", "repository", "create", "filesystem",
		"--path="+repoPath, "-c", "-p", "kopiapwd",
		"--cache-directory="+cachePath, "--no-check-for-updates",
		"--override-hostname=localhost", "--override-username=kopia")

	runKopia("repository connect", "--config-file="+configFile, "repository", "connect", "filesystem",
		"--path="+repoPath, "-p", "kopiapwd", "--cache-directory="+cachePath, "--no-check-for-updates")

	runKopia("snapshot create", "--config-file="+configFile, "snapshot", "create", "-p", "kopiapwd", baseDir,
		"--start-time=2025-05-01 15:20:01 CET", "--end-time=2025-05-01 16:10:02 CET")

	runKopia("server user add", "--config-file="+configFile, "server", "user", "add", "kopia@localhost",
		"--user-password=kopiapwd", "-p", "kopiapwd")

	srv := exec.CommandContext(ctx, bin,
		"--config-file="+configFile, "server", "start", "-p", "kopiapwd",
		"--address=http://0.0.0.0:"+port,
		"--file-log-level=debug",
		"--server-username=kopia",
		"--server-password=kopiapwd",
		"--server-control-username=kopia",
		"--server-control-password=Kopia",
		"--tls-generate-cert",
		"--tls-cert-file", certFile,
		"--tls-key-file", keyFile,
	)
	srv.Stdout = os.Stdout
	srv.Stderr = os.Stderr
	require.NoError(t, srv.Start(), "failed to start kopia server")

	require.Eventually(t, func() bool {
		_, err := os.Stat(certFile)
		return err == nil
	}, 10*time.Second, 200*time.Millisecond, "kopia server did not generate a TLS certificate")

	require.Eventually(t, func() bool {
		conn, err := net.Dial("tcp", net.JoinHostPort(ip, port))
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 30*time.Second, 500*time.Millisecond, "kopia server is not listening on %s:%s", ip, port)

	certContents, err := os.ReadFile(certFile)
	require.NoError(t, err, "failed to read certificate")
	fingerprint, err = hashSHA256(certContents)
	require.NoError(t, err, "failed to extract fingerprint from certificate")

	cleanup = func() {
		shutdown := exec.CommandContext(context.Background(), bin, "server", "shutdown",
			"--server-cert-fingerprint="+fingerprint,
			"--address=https://"+net.JoinHostPort(ip, port),
			"--server-control-username=kopia",
			"--server-control-password=Kopia",
		)
		_ = shutdown.Run()

		if srv.Process != nil {
			_ = srv.Process.Kill()
			_, _ = srv.Process.Wait()
		}
	}

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
			BaseURL:                             fmt.Sprintf("https://%s:%s", ip, port),
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
		BaseURL:                             fmt.Sprintf("https://%s:%s", ip, port),
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

// TestKopiaBinaryPresent verifies that the kopia test executable already exists
// in the kopiametrics/ directory. When it is missing, the test downloads the
// configured kopia release instead of failing the suite, so the binary is only
// fetched once and reused on later runs.
func TestKopiaBinaryPresent(t *testing.T) {
	if _, err := os.Stat(kopiaTestBinaryPath); err == nil {
		t.Logf("kopia test binary already present at %s", kopiaTestBinaryPath)
		return
	}

	t.Logf("kopia test binary not present at %s, downloading %s", kopiaTestBinaryPath, kopiaTestVersion)
	require.NoError(t, downloadKopiaBinary(t), "failed to download kopia binary")
}

// TestKopiaVersion ensures the downloaded kopia executable runs at the expected
// version. If the version does not match (for example an outdated binary was
// left behind), the test re-downloads the configured kopia release rather than
// failing outright. The test only fails when the binary cannot be obtained or
// is still incorrect after the download.
func TestKopiaVersion(t *testing.T) {
	if _, err := os.Stat(kopiaTestBinaryPath); err != nil {
		t.Logf("kopia test binary missing at %s, downloading %s", kopiaTestBinaryPath, kopiaTestVersion)
		require.NoError(t, downloadKopiaBinary(t), "failed to download kopia binary")
	}

	want := strings.TrimPrefix(kopiaTestVersion, "v")

	got, err := kopiaBinaryVersion(t, kopiaTestBinaryPath)
	require.NoError(t, err, "failed to read kopia version")

	if got != want {
		t.Logf("kopia version mismatch: got %q want %q, re-downloading %s", got, want, kopiaTestVersion)
		require.NoError(t, downloadKopiaBinary(t), "failed to re-download kopia binary")

		got, err = kopiaBinaryVersion(t, kopiaTestBinaryPath)
		require.NoError(t, err, "failed to read kopia version after re-download")
		require.Equal(t, want, got, "kopia version still incorrect after re-download")
	}

	t.Logf("kopia test binary is at expected version %s", kopiaTestVersion)
}
