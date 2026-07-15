// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kopia/kopia/fs"
	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/snapshot"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kopia-go-exporter/config"
	"kopia-go-exporter/logger"
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

// freeTestPort returns an available TCP port on the loopback interface. It asks
// the operating system to assign a free port (by binding to :0), confirms the
// chosen port is not already in use, and returns it as a string. There is a
// small window between releasing the probe listener and the test binding it,
// but this avoids the hardcoded port colliding with other local services.
func freeTestPort(t *testing.T) string {
	t.Helper()

	lc := &net.ListenConfig{}
	l, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", "0"))
	require.NoError(t, err, "failed to allocate a free port")

	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)

	require.NoError(t, l.Close(), "failed to release the probe listener")

	d := &net.Dialer{Timeout: 200 * time.Millisecond}
	conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", port))
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected port %s to be free but something is already listening", port)
	}

	return port
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

	ip = "127.0.0.1"
	port = freeTestPort(t)

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
		conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", net.JoinHostPort(ip, port))
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
		shutdown := exec.CommandContext(context.Background(), bin, "server", "shutdown", //nolint:gosec
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

func logGatheredMetrics(t *testing.T, families []*dto.MetricFamily) {
	t.Helper()
	if len(families) == 0 {
		t.Log("gathered metrics: (empty)")
		return
	}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			labels := make(map[string]string)
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			switch {
			case m.GetGauge() != nil:
				t.Logf("metric %s %v = %v", f.GetName(), labels, m.GetGauge().GetValue())
			case m.GetCounter() != nil:
				t.Logf("metric %s %v = %v", f.GetName(), labels, m.GetCounter().GetValue())
			default:
				t.Logf("metric %s %v (unknown type)", f.GetName(), labels)
			}
		}
	}
}

func TestNewKopiaClient(t *testing.T) {
	cfg := &config.Config{}
	k, err := NewKopiaClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { k.Disconnect(context.Background()) })
	assert.NotNil(t, k)
	assert.False(t, k.isConnected)
}

func TestNewKopiaClient_TempDirFailure(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent-kopia-tmp-dir")
	_, err := NewKopiaClient(&config.Config{})
	assert.Error(t, err)
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

	k, err := NewKopiaClient(&config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { k.Disconnect(context.Background()) })
	reg := prometheus.NewRegistry()
	k.RegisterKopiaMetrics(reg)

	for _, mn := range metricNames {
		collector := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: mn,
			Help: "whatever",
		})
		err := reg.Register(collector)
		assert.Error(t, err, "Expected metric '%s' to already be registered", mn)
	}
}

func TestKopiaClient_Connect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cleanup, fingerprint, ip, port := setupTestKopia(t)
	defer cleanup()

	configFile := filepath.Join(t.TempDir(), "repo.config")

	logger.Reset(nil)

	k := &KopiaClient{
		isConnected: false,
	}

	ctx := context.Background()
	opts := repo.ConnectOptions{
		ClientOptions: repo.ClientOptions{
			Username: "kopia", //nolint:goconst
			Hostname: "localhost", //nolint:goconst
		},
	}
	serverInfo := repo.APIServerInfo{
		BaseURL:                             fmt.Sprintf("https://%s:%s", ip, port),
		TrustedServerCertificateFingerprint: fingerprint,
	}

	err := repo.ConnectAPIServer(ctx, configFile, &serverInfo, "kopiapwd", &opts)
	require.NoError(t, err, "Failed to connect API server")

	k.repo, err = repo.Open(ctx, configFile, "kopiapwd", nil)
	require.NoError(t, err, "Failed to open repository")
	k.isConnected = true

	assert.True(t, k.isConnected)
}

func TestKopiaClient_Disconnect(t *testing.T) {
	logger.Reset(nil)

	k, err := NewKopiaClient(&config.Config{})
	require.NoError(t, err)
	assert.False(t, k.isConnected)

	k.isConnected = true
	k.Disconnect(context.Background())
	assert.False(t, k.isConnected)
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

func TestSetSnapshotMetrics_RetentionFiltering(t *testing.T) {
	cfg := &config.Config{
		Kopia: config.KopiaConfig{
			Retentions: []string{"daily"},
		},
	}
	cfg.Exporter.Metrics.Prefix = "kopia_go_exporter" //nolint:goconst

	logger.Reset(nil)

	k, err := NewKopiaClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { k.Disconnect(context.Background()) })
	reg := prometheus.NewRegistry()
	k.RegisterKopiaMetrics(reg)

	now := fs.UTCTimestampFromTime(time.Now())
	m := &snapshot.Manifest{
		Source:           snapshot.SourceInfo{Host: "testhost", UserName: "testuser", Path: "/test/path"},
		StartTime:        now,
		EndTime:          now,
		RetentionReasons: []string{"latest"},
		Stats: snapshot.Stats{
			TotalFileCount:      10,
			TotalDirectoryCount: 2,
			TotalFileSize:       1024,
		},
	}

	k.setSnapshotMetrics(m, false)

	families, err := reg.Gather()
	require.NoError(t, err)
	logGatheredMetrics(t, families)
	assert.Empty(t, families, "no metrics should be set when retention is filtered out")
}

func TestSetSnapshotMetrics_KeepAllRetentions(t *testing.T) {
	cfg := &config.Config{
		Kopia: config.KopiaConfig{
			Retentions: []string{"daily"},
		},
	}
	cfg.Exporter.Metrics.Prefix = "kopia_go_exporter"

	logger.Reset(nil)

	k, err := NewKopiaClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { k.Disconnect(context.Background()) })
	reg := prometheus.NewRegistry()
	k.RegisterKopiaMetrics(reg)

	now := fs.UTCTimestampFromTime(time.Now())
	m := &snapshot.Manifest{
		Source:           snapshot.SourceInfo{Host: "testhost", UserName: "testuser", Path: "/test/path"},
		StartTime:        now,
		EndTime:          now,
		RetentionReasons: []string{"latest"},
		Stats: snapshot.Stats{
			TotalFileCount:      10,
			TotalDirectoryCount: 2,
			TotalFileSize:       1024,
		},
	}

	k.setSnapshotMetrics(m, true)

	families, err := reg.Gather()
	require.NoError(t, err)
	logGatheredMetrics(t, families)
	require.NotEmpty(t, families, "metrics should be set when keepAllRetentions is true")

	familyMap := make(map[string]*dto.MetricFamily)
	for _, f := range families {
		familyMap[f.GetName()] = f
	}

	gauge := familyMap["kopia_go_exporter_file_count"]
	require.NotNil(t, gauge)
	require.NotEmpty(t, gauge.GetMetric())
	assert.Equal(t, float64(10), gauge.GetMetric()[0].GetGauge().GetValue())
}

func TestRunOnce_ConnectFails(t *testing.T) {
	logger.Reset(nil)

	cfg := &config.Config{
		Kopia: config.KopiaConfig{
			Password: "wrong",
			APIServer: config.APIServerConfig{
				RepositoryURL: "https://127.0.0.1:1",
				Fingerprint:   "0000000000000000000000000000000000000000000000000000000000000000",
				Hostname:      "localhost",
				Username:      "kopia",
			},
		},
	}

	k, err := NewKopiaClient(cfg)
	require.NoError(t, err)
	k.configFile = filepath.Join(t.TempDir(), "nonexistent.config")
	t.Cleanup(func() { k.Disconnect(context.Background()) })

	err = k.RunOnce(context.Background())
	assert.Error(t, err, "RunOnce should fail when Connect fails")
	assert.False(t, k.isConnected)
}

func TestRunOnce_EmptyRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	bin := KopiaTestBinaryPath()
	if _, err := os.Stat(bin); err != nil {
		require.NoError(t, downloadKopiaBinary(t), "failed to obtain kopia test binary")
	}

	bin, err := filepath.Abs(bin)
	require.NoError(t, err)

	baseDir := t.TempDir()
	repoPath := filepath.Join(baseDir, "repo")
	cachePath := filepath.Join(baseDir, "cache")
	configFile := filepath.Join(baseDir, "repo.config")
	password := "kopiapwd" //nolint:goconst

	cmd := exec.CommandContext(context.Background(), bin, "repository", "create", "filesystem",
		"--path="+repoPath, "-c", "-p", password,
		"--cache-directory="+cachePath, "--no-check-for-updates",
		"--override-hostname=localhost", "--override-username=kopia")
	cmd.Dir = baseDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "repository create failed: %s", string(out))

	cmd = exec.CommandContext(context.Background(), bin, "--config-file="+configFile, "repository", "connect", "filesystem",
		"--path="+repoPath, "-p", password, "--cache-directory="+cachePath, "--no-check-for-updates")
	cmd.Dir = baseDir
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "repository connect failed: %s", string(out))

	logger.Reset(nil)

	cfg := &config.Config{
		Kopia: config.KopiaConfig{
			Password:   password,
			Retentions: []string{},
		},
	}
	cfg.Exporter.Metrics.Prefix = "kopia_go_exporter"

	k := &KopiaClient{
		isConnected: false,
		cfg:         cfg,
	}

	ctx := context.Background()
	k.repo, err = repo.Open(ctx, configFile, password, nil)
	require.NoError(t, err)
	k.isConnected = true

	reg := prometheus.NewRegistry()
	k.RegisterKopiaMetrics(reg)

	require.NoError(t, k.RunOnce(ctx), "RunOnce should succeed on empty repo")

	families, err := reg.Gather()
	require.NoError(t, err)
	logGatheredMetrics(t, families)
	assert.Empty(t, families, "no metrics should be set for an empty repo")
}

func TestConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cleanup, fingerprint, ip, port := setupTestKopia(t)
	defer cleanup()

	configFile := filepath.Join(t.TempDir(), "repo.config")

	cfg := &config.Config{
		Kopia: config.KopiaConfig{
			Password: "kopiapwd",
			APIServer: config.APIServerConfig{
				RepositoryURL: fmt.Sprintf("https://%s:%s", ip, port),
				Fingerprint:   fingerprint,
				Hostname:      "localhost",
				Username:      "kopia",
			},
		},
	}

	logger.Reset(nil)

	k, err := NewKopiaClient(cfg)
	require.NoError(t, err)
	k.configFile = configFile
	t.Cleanup(func() { k.Disconnect(context.Background()) })

	err = k.Connect(context.Background())
	require.NoError(t, err, "Connect should succeed")
	assert.True(t, k.isConnected)
	assert.NotNil(t, k.repo)
}

func TestConnect_OpenFails(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cleanup, fingerprint, ip, port := setupTestKopia(t)

	configFile := filepath.Join(t.TempDir(), "repo.config")

	cfg := &config.Config{
		Kopia: config.KopiaConfig{
			Password: "kopiapwd",
			APIServer: config.APIServerConfig{
				RepositoryURL: fmt.Sprintf("https://%s:%s", ip, port),
				Fingerprint:   fingerprint,
				Hostname:      "localhost",
				Username:      "kopia",
			},
		},
	}

	logger.Reset(nil)

	k, err := NewKopiaClient(cfg)
	require.NoError(t, err)
	k.configFile = configFile
	t.Cleanup(func() { k.Disconnect(context.Background()) })

	err = k.Connect(context.Background())
	require.NoError(t, err, "initial Connect should succeed")
	assert.True(t, k.isConnected)
	require.NoError(t, k.repo.Close(context.Background()))

	cleanup()

	cfg2 := &config.Config{
		Kopia: config.KopiaConfig{
			Password: "kopiapwd",
			APIServer: config.APIServerConfig{
				RepositoryURL: fmt.Sprintf("https://%s:%s", ip, port),
				Fingerprint:   fingerprint,
				Hostname:      "localhost",
				Username:      "kopia",
			},
		},
	}
	k2, err := NewKopiaClient(cfg2)
	require.NoError(t, err)
	k2.configFile = configFile
	t.Cleanup(func() { k2.Disconnect(context.Background()) })

	err = k2.Connect(context.Background())
	assert.Error(t, err, "Connect should fail after server is stopped")
	assert.False(t, k2.isConnected)
}

func TestRunOnce_ConnectsAutomatically(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cleanup, fingerprint, ip, port := setupTestKopia(t)
	defer cleanup()

	configFile := filepath.Join(t.TempDir(), "repo.config")

	cfg := &config.Config{
		Kopia: config.KopiaConfig{
			Password:   "kopiapwd",
			Retentions: []string{},
			APIServer: config.APIServerConfig{
				RepositoryURL: fmt.Sprintf("https://%s:%s", ip, port),
				Fingerprint:   fingerprint,
				Hostname:      "localhost",
				Username:      "kopia",
			},
		},
	}
	cfg.Exporter.Metrics.Prefix = "kopia_go_exporter"

	logger.Reset(nil)

	k, err := NewKopiaClient(cfg)
	require.NoError(t, err)
	k.configFile = configFile
	t.Cleanup(func() { k.Disconnect(context.Background()) })
	reg := prometheus.NewRegistry()
	k.RegisterKopiaMetrics(reg)

	require.False(t, k.isConnected, "IsConnected should start false")
	require.NoError(t, k.RunOnce(context.Background()), "RunOnce should succeed with auto-connect")
	assert.True(t, k.isConnected, "RunOnce should have connected the client")
	require.NotNil(t, k.repo, "Repo should be set after auto-connect")
}

// setupTestRepo creates a local Kopia filesystem repository and a separate
// data directory with a known structure (1 subdir, 3 non-empty files), then
// takes a snapshot of the data directory with explicit start/end times. It
// returns the config file path, the data directory path, and the repository
// password. This helper does NOT start a Kopia API server — it is intended
// for tests that connect directly to the repository via the Go API.
func setupTestRepo(t *testing.T) (configFile, sourceDir, password string) {
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
	configFile = filepath.Join(baseDir, "repo.config")
	password = "kopiapwd"

	// Create a data directory with a known structure, separate from the repo.
	sourceDir = t.TempDir()
	subDir := filepath.Join(sourceDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	for _, name := range []string{"file1.txt", "file2.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, name), []byte("dummy content for "+name), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "file3.txt"), []byte("dummy content for file3.txt"), 0o600))

	runKopia := func(name string, args ...string) {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = baseDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s failed: %s", name, string(out))
	}

	runKopia("repository create", "repository", "create", "filesystem",
		"--path="+repoPath, "-c", "-p", password,
		"--cache-directory="+cachePath, "--no-check-for-updates",
		"--override-hostname=localhost", "--override-username=kopia")

	runKopia("repository connect", "--config-file="+configFile, "repository", "connect", "filesystem",
		"--path="+repoPath, "-p", password, "--cache-directory="+cachePath, "--no-check-for-updates")

	runKopia("snapshot create", "--config-file="+configFile, "snapshot", "create", "-p", password, sourceDir,
		"--start-time=2025-05-01 15:20:01 CET", "--end-time=2025-05-01 16:10:02 CET")

	return configFile, sourceDir, password
}

func TestRunOnceMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	configFile, sourceDir, password := setupTestRepo(t)

	cfg := &config.Config{
		Exporter: config.ExporterConfig{
			Port: 9090,
		},
		Kopia: config.KopiaConfig{
			Password:   password,
			Retentions: []string{},
		},
	}
	cfg.Exporter.Metrics.Prefix = "kopia_go_exporter"

	logger.Reset(nil)

	k := &KopiaClient{
		isConnected: false,
		cfg:         cfg,
	}

	var err error
	ctx := context.Background()
	k.repo, err = repo.Open(ctx, configFile, password, nil)
	require.NoError(t, err, "Failed to open repository")
	k.isConnected = true

	reg := prometheus.NewRegistry()
	k.RegisterKopiaMetrics(reg)

	require.NoError(t, k.RunOnce(ctx), "RunOnce failed")

	families, err := reg.Gather()
	require.NoError(t, err, "Failed to gather metrics")
	logGatheredMetrics(t, families)

	familyMap := make(map[string]*dto.MetricFamily)
	for _, f := range families {
		familyMap[f.GetName()] = f
	}

	expectedMetrics := []string{
		"kopia_go_exporter_total_size",
		"kopia_go_exporter_file_count",
		"kopia_go_exporter_dir_count",
		"kopia_go_exporter_error_count",
		"kopia_go_exporter_backup_duration",
		"kopia_go_exporter_backup_start_time",
		"kopia_go_exporter_backup_end_time",
	}

	for _, name := range expectedMetrics {
		fam, ok := familyMap[name]
		require.True(t, ok, "metric %s not found in registry", name)
		require.NotEmpty(t, fam.GetMetric(), "metric %s has no samples", name)

		m := fam.GetMetric()[0]
		labels := make(map[string]string)
		for _, lp := range m.GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		assert.NotEmpty(t, labels["host"], "%s: host label should not be empty", name)
		assert.NotEmpty(t, labels["user"], "%s: user label should not be empty", name)
		assert.Equal(t, sourceDir, labels["path"], "%s: unexpected path label", name)
		assert.NotEmpty(t, labels["retention"], "%s: retention label should not be empty", name)
	}

	cet := time.FixedZone("CET", 3600)
	expectedStart := time.Date(2025, 5, 1, 15, 20, 1, 0, cet).Unix()
	expectedEnd := time.Date(2025, 5, 1, 16, 10, 2, 0, cet).Unix()

	startTime := familyMap["kopia_go_exporter_backup_start_time"].GetMetric()[0].GetGauge().GetValue()
	endTime := familyMap["kopia_go_exporter_backup_end_time"].GetMetric()[0].GetGauge().GetValue()
	assert.InDelta(t, float64(expectedStart), startTime, 1, "backup_start_time should match hardcoded start time")
	assert.InDelta(t, float64(expectedEnd), endTime, 1, "backup_end_time should match hardcoded end time")

	duration := familyMap["kopia_go_exporter_backup_duration"].GetMetric()[0].GetGauge().GetValue()
	assert.Greater(t, duration, float64(0), "backup_duration should be positive")

	fileCount := familyMap["kopia_go_exporter_file_count"].GetMetric()[0].GetGauge().GetValue()
	assert.Equal(t, float64(3), fileCount, "file_count should be 3 (file1.txt, file2.txt, file3.txt)")

	dirCount := familyMap["kopia_go_exporter_dir_count"].GetMetric()[0].GetGauge().GetValue()
	assert.Equal(t, float64(2), dirCount, "dir_count should be 2 (root + subdir)")

	totalSize := familyMap["kopia_go_exporter_total_size"].GetMetric()[0].GetGauge().GetValue()
	assert.Equal(t, float64(81), totalSize, "total_size should be 81 bytes")

	errorCount := familyMap["kopia_go_exporter_error_count"].GetMetric()[0].GetGauge().GetValue()
	assert.Equal(t, float64(0), errorCount, "error_count should be 0")
}
