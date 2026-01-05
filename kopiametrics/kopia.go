package kopiametrics

import (
	"context"
	"fmt"
	"kopia-go-exporter/modconfig"
	"slices"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/content"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

var Logger zerolog.Logger

type KopiaMetrics struct {
	TotalSize       *prometheus.GaugeVec
	FileCount       *prometheus.GaugeVec
	DirCount        *prometheus.GaugeVec
	ErrorCount      *prometheus.GaugeVec
	BackupDuration  *prometheus.GaugeVec
	BackupStartTime *prometheus.GaugeVec
	BackupEndTime   *prometheus.GaugeVec
}

type KopiaClient struct {
	Ctx         context.Context
	IsConnected bool
	Opts        repo.ConnectOptions
	ServerInfo  repo.APIServerInfo
	Repo        repo.Repository
	Metrics     KopiaMetrics
}

func NewKopiaClient() *KopiaClient {
	k := new(KopiaClient)

	return k
}

func (k *KopiaClient) RegisterKopiaMetrics(reg *prometheus.Registry) {
	k.Metrics.TotalSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "total_size",
			Help:      "Total size of the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.TotalSize)
	k.Metrics.FileCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "file_count",
			Help:      "Number of files in the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.FileCount)
	k.Metrics.DirCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "dir_count",
			Help:      "Number of directories in the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.DirCount)
	k.Metrics.ErrorCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "error_count",
			Help:      "Number of errors in the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.ErrorCount)
	k.Metrics.BackupDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_duration",
			Help:      "Duration of the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.BackupDuration)
	k.Metrics.BackupStartTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_start_time",
			Help:      "Start time of the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.BackupStartTime)
	k.Metrics.BackupEndTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_end_time",
			Help:      "End time of the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.BackupEndTime)
}

func (k *KopiaClient) GenerateConfigFile() error {
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
	if err := repo.ConnectAPIServer(k.Ctx, modconfig.Cfg.Kopia.ConfigFile, &serverInfo, modconfig.Cfg.Kopia.Password, &opts); err != nil {
		Logger.Error().Err(err).Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Failed to generate configFile")
		return err
	}
	Logger.Debug().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Successfully generated configFile")
	return nil
}

func (k *KopiaClient) Connect() error {
	var err error

	if !modconfig.Cfg.Kopia.ConnectWithConfigFile {
		if err := k.GenerateConfigFile(); err != nil {
			Logger.Error().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Err(err).Msg("Failed to launch repository server")
			k.IsConnected = false
			return err
		}
	}
	Logger.Debug().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Try to connect to server")
	k.Repo, err = repo.Open(k.Ctx, modconfig.Cfg.Kopia.ConfigFile, modconfig.Cfg.Kopia.Password, nil)
	if err != nil {
		Logger.Error().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Err(err).Msg("Failed to open repository")
		k.IsConnected = false
		return err
	}
	k.IsConnected = true
	return nil
}

func (k *KopiaClient) RunOnce() error {
	keepAllRetentions := (0 == len(modconfig.Cfg.Kopia.Retentions))
	if !k.IsConnected {
		if err := k.Connect(); err != nil {
			return err
		}
	}
	// FIXME : if IsConnected == false, set error status to 1 (in metrics) and return

	// List all snapshot manifests (nil -> all sources)
	manifestsIds, err := snapshot.ListSnapshotManifests(k.Ctx, k.Repo, nil, nil)
	if err != nil {
		Logger.Error().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Err(err).Msg("failed to list snapshot manifests")
		return err
	}

	manifests, err := snapshot.LoadSnapshots(k.Ctx, k.Repo, manifestsIds)
	if err != nil {
		Logger.Error().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Err(err).Msg("failed to snapshot manifests")
		return err
	}

	for _, snapshotGroup := range snapshot.GroupBySource(manifests) {
		snapshotGroup = snapshot.SortByTime(snapshotGroup, true)
		src := snapshotGroup[0].Source

		// compute retention reason
		pol, _, _, err := policy.GetEffectivePolicy(k.Ctx, k.Repo, src)
		if err != nil {
			Logger.Error().Str("Source", fmt.Sprintf("%v", src)).Msg("Unable to determine effective policy")
		} else {
			pol.RetentionPolicy.ComputeRetentionReasons(snapshotGroup)
		}

		// Iterate over snapshotGroup of manifests
		for _, m := range snapshotGroup {
			// fmt.Printf("ID: %v  Source: %v  Time: %v  Retentions: %v\n", m.ID, m.Source, m.StartTime, m.RetentionReasons)
			for _, rr := range m.RetentionReasons {
				if slices.Contains(modconfig.Cfg.Kopia.Retentions, rr) || keepAllRetentions {
					labels := prometheus.Labels{"host": m.Source.Host, "path": m.Source.Path, "user": m.Source.UserName, "retention": rr}
					k.Metrics.BackupStartTime.With(labels).Set(float64(m.StartTime.ToTime().Unix()))
					k.Metrics.BackupEndTime.With(labels).Set(float64(m.EndTime.ToTime().Unix()))
					k.Metrics.BackupDuration.With(labels).Set(float64((m.EndTime - m.StartTime).ToTime().Unix()))
					k.Metrics.DirCount.With(labels).Set(float64(m.Stats.TotalDirectoryCount))
					k.Metrics.ErrorCount.With(labels).Set(float64(m.Stats.ErrorCount))
					k.Metrics.FileCount.With(labels).Set(float64(m.Stats.TotalFileCount))
					k.Metrics.TotalSize.With(labels).Set(float64(m.Stats.TotalFileSize))
				}
			}
		}
	}
	return nil
}

func (k *KopiaClient) Disconnect() {
	repo.Disconnect(k.Ctx, modconfig.Cfg.Kopia.ConfigFile)
	Logger.Debug().Str("ConfigFile", modconfig.Cfg.Kopia.ConfigFile).Msg("Disconnected from server")
	k.IsConnected = false
}

func main() {
	k := NewKopiaClient()
	k.RunOnce()
}
