package kopiametrics

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"kopia-go-exporter/modconfig"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/kopia/kopia/repo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// hashSHA256 computes the SHA256 hash of a string.
func hashSHA256(pemContent []byte) (string, error) {
	block, _ := pem.Decode(pemContent)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM contents")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate bloack: %w", err)
	}

	// Compute SHA-256 fingerprint
	fingerprint := sha256.Sum256(cert.Raw)

	return fmt.Sprintf("%x", fingerprint), nil
}

func setupTestKopia(t *testing.T) (func(), string, string, nat.Port) {
	ctx := context.Background()

	kopiaRunScript := `
#! /bin/bash

rm -rf /tmp/repo
rm -rf /tmp/cache

kopia repository create filesystem --path="/tmp/repo" -c -p kopiapwd --cache-directory="/tmp/cache" --no-check-for-updates --override-hostname=localhost --override-username=kopia

kopia --config-file="/tmp/repo.config" repository connect filesystem --path="/tmp/repo" -p kopiapwd --cache-directory="/tmp/cache" --no-check-for-updates

kopia --config-file="/tmp/repo.config" snapshot create -p kopiapwd $(pwd) --start-time="2025-05-01 15:20:01 CET" --end-time="2025-05-01 16:10:02 CET"

kopia --config-file="/tmp/repo.config" server user add kopia@localhost --user-password=kopiapwd -p kopiapwd

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
	assert.NoError(t, err, "Failed to create new network")

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
			wait.ForListeningPort("51515/tcp").WithStartupTimeout(time.Second*30)),
			wait.ForFile("/tmp/my.cert").WithPollInterval(500*time.Millisecond).WithStartupTimeout(time.Second*5),
		),
	)
	if err != nil {
		assert.NoError(t, nw.Remove(ctx), "Failed to remove network")
		assert.NoError(t, err, "Something went wrong when creating a kopia server")
		return nil, "", "", ""
	}
	cleanup0 := func() {
		t.Logf("Cannot get fingerprint; container will probably not be cleaned properly")
		ctr.Terminate(ctx)
		assert.NoError(t, nw.Remove(ctx), "Failed to remove network")
		assert.NoError(t, os.Remove("/tmp/repo.config"), "Failed to remove file /tmp/repo.config")
	}

	certFile, err := ctr.CopyFileFromContainer(context.Background(), "/tmp/my.cert")
	if err != nil {
		cleanup0()
		assert.NoError(t, err, "Failed to read /tmp/my.cert from inside the container")
		return nil, "", "", ""
	}

	certContents, err := io.ReadAll(certFile)
	if err != nil {
		cleanup0()
		assert.NoError(t, err, "Failed to get /tmp/my.cert contents")
		return nil, "", "", ""
	}

	fingerprint, err := hashSHA256(certContents)
	if err != nil {
		cleanup0()
		assert.NoError(t, err, "Failed to extract fingerprint from certificate")
		return nil, "", "", ""
	}

	ip, err := ctr.Host(ctx)
	if err != nil {
		cleanup0()
		assert.NoError(t, err, "Failed to extract host ip")
		return nil, "", "", ""
	}

	port, err := ctr.MappedPort(ctx, "51515/tcp")
	if err != nil {
		cleanup0()
		assert.NoError(t, err, "Failed to extract port")
		return nil, "", "", ""
	}
	cleanup := func() {
		ctr.Exec(ctx, []string{"bash", "-c", fmt.Sprintf(
			"kopia server shutdown --server-cert-fingerprint=%s --address=https://%s:%s --server-control-username=kopia --server-control-password=Kopia",
			fingerprint,
			ip,
			port.Port(),
		)})
		ctr.Terminate(ctx)
		assert.NoError(t, nw.Remove(ctx), "Failed to remove network")
	}
	return cleanup, fingerprint, ip, port
}

func TestNewKopiaClient(t *testing.T) {
	tests := []struct {
		name string
		want *KopiaClient
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewKopiaClient(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewKopiaClient() = %v, want %v", got, tt.want)
			}
		})
	}
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
	t.Run("All metrics are registered", func(t *testing.T) {
		k := &KopiaClient{}
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
	})
}

func TestKopiaClient_GenerateConfigFile(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}
			k.GenerateConfigFile()
		})
	}
}

func TestKopiaClient_Connect(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}

	cleanup, fingerprint, ip, port := setupTestKopia(t)
	assert.NotNil(t, cleanup, "Failed to setup Kopia test server")
	defer cleanup()

	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "Connect to local Kopia test server",
			fields: fields{
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
			},
		},
	}

	modconfig.Cfg.LogLevel = "debug"
	modconfig.Cfg.Kopia.APIServer.RepositoryURL = fmt.Sprintf("https://%s:%s", ip, port.Port())
	modconfig.Cfg.Kopia.ConfigFile = "/tmp/repo2.config"
	modconfig.Cfg.Kopia.ConnectWithConfigFile = false
	modconfig.Cfg.Kopia.Password = "kopiapwd"
	modconfig.Cfg.Kopia.APIServer.Username = "kopia"
	modconfig.Cfg.Kopia.APIServer.Hostname = "localhost"
	modconfig.Cfg.Kopia.APIServer.Fingerprint = fingerprint

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}

			err := k.Connect()
			assert.NoError(t, err, "Failed to connect")
			assert.True(t, k.IsConnected, "After connection, isConnect should be true; found false")
		})
	}
}

func TestKopiaClient_RunOnce(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}
			k.RunOnce()
		})
	}
}

func TestKopiaClient_Disconnect(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}
			k.Disconnect()
		})
	}
}

func Test_main(t *testing.T) {
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			main()
		})
	}
}
