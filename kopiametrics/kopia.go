package kopiametrics

import (
	"context"
	"fmt"
	"kopia-go-exporter/exporter"
	"kopia-go-exporter/modconfig"
	"os"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/content"
	"github.com/kopia/kopia/snapshot"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

var Logger zerolog.Logger

type KopiaClient struct {
	Ctx         context.Context
	IsConnected bool
	Opts        repo.ConnectOptions
	ServerInfo  repo.APIServerInfo
	Repo        repo.Repository
	Metrics     *exporter.KopiaMetrics
}

func NewKopiaClient(m *exporter.KopiaMetrics) *KopiaClient {
	k := new(KopiaClient)
	k.Metrics = m

	return k
}

func (k *KopiaClient) GenerateConfigFile() {
	k.Ctx = context.Background()

	opts := repo.ConnectOptions{
		ClientOptions: repo.ClientOptions{
			Username: modconfig.Cfg.Kopia.APIServer.Username,
			Hostname: modconfig.Cfg.Kopia.APIServer.Hostname,
		},
		CachingOptions: content.CachingOptions{},
	}
	serverInfo := repo.APIServerInfo{
		BaseURL:                             modconfig.Cfg.Kopia.APIServer.RepositoryURL,
		TrustedServerCertificateFingerprint: modconfig.Cfg.Kopia.APIServer.Fingerprint,
	}

	// Connect to Kopia Repository API Server
	Logger.Debug().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Str("URL", modconfig.Cfg.Kopia.APIServer.RepositoryURL).Msg("Generate ConfigFile and try to connect to server")
	err := repo.ConnectAPIServer(k.Ctx, modconfig.Cfg.Kopia.ConfigFile, &serverInfo, modconfig.Cfg.Kopia.Password, &opts)
	if err != nil {
		Logger.Error().Err(err).Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Failed to generate configFile")
		os.Exit(1)
	}
	Logger.Debug().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Successfully generated configFile")
}

func (k *KopiaClient) Connect() {
	if !modconfig.Cfg.Kopia.ConnectWithConfigFile {
		k.GenerateConfigFile()
	}
	Logger.Debug().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Try to connect to server")
	var err error
	k.Repo, err = repo.Open(k.Ctx, modconfig.Cfg.Kopia.ConfigFile, modconfig.Cfg.Kopia.Password, nil)
	if err != nil {
		Logger.Error().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Err(err).Msg("Failed to open repository")
		k.IsConnected = false
		return
	}
	k.IsConnected = true
}

func (k *KopiaClient) RunOnce() {
	if !k.IsConnected {
		k.Connect()
	}
	// FIXME : if IsConnected == false, set error status to 1 (in metrics) and return

	// List all snapshot manifests (nil -> all sources)
	manifestsIds, err := snapshot.ListSnapshotManifests(k.Ctx, k.Repo, nil, nil)
	if err != nil {
		Logger.Error().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Err(err).Msg("failed to list snapshot manifests")
		return
	}

	manifests, err := snapshot.LoadSnapshots(k.Ctx, k.Repo, manifestsIds)
	if err != nil {
		Logger.Error().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Err(err).Msg("failed to snapshot manifests")
		return
	}

	for _, m := range manifests {
		fmt.Printf("ID: %v  Source: %v  Time: %v\n", m.ID, m.Source, m.StartTime)
		labels := prometheus.Labels{"host": m.Source.Host, "path": m.Source.Path, "user": m.Source.UserName}
		k.Metrics.BackupStartTime.With(labels).Set(float64(m.StartTime.ToTime().Unix()))
		// backup_duration
		// backup_end_time
		// backup_start_time
		// dir_count
		// error_count
		// file_count
		// total_size
	}
}

func (k *KopiaClient) Disconnect() {
	repo.Disconnect(k.Ctx, modconfig.Cfg.Kopia.ConfigFile)
	Logger.Debug().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Disconnected from server")
	k.IsConnected = false
}

func main() {
	k := NewKopiaClient(nil)
	k.RunOnce()
}
