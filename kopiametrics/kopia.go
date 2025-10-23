package kopiametrics

import (
	"context"
	"fmt"
	"kopia-go-exporter/exporter"
	"kopia-go-exporter/modconfig"

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

func (k *KopiaClient) Connect() {
	k.Ctx = context.Background()

	// Connect to Kopia Repository API Server
	err := repo.ConnectAPIServer(k.Ctx, modconfig.Cfg.Kopia.ConfigFile, &k.ServerInfo, modconfig.Cfg.Kopia.Password, &k.Opts)
	if err != nil {
		Logger.Error().Err(err).Msg("Failed to connect to Kopia API server")
		k.IsConnected = false
		return
	}
	k.IsConnected = true
	Logger.Debug().Str("URL", modconfig.Cfg.Kopia.RepositoryURL).Msg("Connected to Kopia server")
}

func NewKopiaClient(m *exporter.KopiaMetrics) *KopiaClient {
	k := new(KopiaClient)
	k.Metrics = m
	k.IsConnected = false
	k.Opts = repo.ConnectOptions{
		repo.ClientOptions{
			Username: modconfig.Cfg.Kopia.Username,
			Hostname: modconfig.Cfg.Kopia.Hostname,
		},
		content.CachingOptions{},
	}
	k.ServerInfo = repo.APIServerInfo{
		BaseURL:                             modconfig.Cfg.Kopia.RepositoryURL,
		TrustedServerCertificateFingerprint: modconfig.Cfg.Kopia.Fingerprint,
	}

	return k
}

func (k *KopiaClient) RunOnce() {
	if !k.IsConnected {
		Logger.Debug().Str("URL", modconfig.Cfg.Kopia.RepositoryURL).Msg("Try to connect to server")
		k.Connect()
		var err error
		k.Repo, err = repo.Open(k.Ctx, modconfig.Cfg.Kopia.ConfigFile, modconfig.Cfg.Kopia.Password, nil)
		if err != nil {
			Logger.Error().Str("URL", modconfig.Cfg.Kopia.RepositoryURL).Err(err).Msg("Failed to open repository")
			k.IsConnected = false
			return
		}
	}

	manifestsIds, err := snapshot.ListSnapshotManifests(k.Ctx, k.Repo, nil, nil)
	if err != nil {
		Logger.Error().Str("URL", modconfig.Cfg.Kopia.RepositoryURL).Err(err).Msg("failed to list snapshot manifests")
		return
	}

	manifests, err := snapshot.LoadSnapshots(k.Ctx, k.Repo, manifestsIds)
	if err != nil {
		Logger.Error().Str("URL", modconfig.Cfg.Kopia.RepositoryURL).Err(err).Msg("failed to snapshot manifests")
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
} // List all snapshot manifests (nil -> all sources)

func (k *KopiaClient) Disconnect() {
	repo.Disconnect(k.Ctx, modconfig.Cfg.Kopia.ConfigFile)
	Logger.Debug().Str("URL", modconfig.Cfg.Kopia.RepositoryURL).Msg("Disconnected from server")
}

func main() {
	k := NewKopiaClient(nil)
	k.RunOnce()
}
