package kopiametrics

import (
	"context"
	"fmt"
	"log/slog"
	"kopia-go-exporter/config"
	"slices"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/content"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/prometheus/client_golang/prometheus"
)

var Logger *slog.Logger

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
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      "total_size",
			Help:      "Total size of the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.TotalSize)
	k.Metrics.FileCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      "file_count",
			Help:      "Number of files in the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.FileCount)
	k.Metrics.DirCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      "dir_count",
			Help:      "Number of directories in the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.DirCount)
	k.Metrics.ErrorCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      "error_count",
			Help:      "Number of errors in the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.ErrorCount)
	k.Metrics.BackupDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_duration",
			Help:      "Duration of the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.BackupDuration)
	k.Metrics.BackupStartTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_start_time",
			Help:      "Start time of the backup",
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(k.Metrics.BackupStartTime)
	k.Metrics.BackupEndTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
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
			Username: config.Cfg.Kopia.APIServer.Username,
			Hostname: config.Cfg.Kopia.APIServer.Hostname,
		},
		CachingOptions: content.CachingOptions{},
	}
	serverInfo := repo.APIServerInfo{
		BaseURL:                             config.Cfg.Kopia.APIServer.RepositoryURL,
		TrustedServerCertificateFingerprint: config.Cfg.Kopia.APIServer.Fingerprint,
	}

	// Connect to Kopia Repository API Server
	Logger.Debug("Generate ConfigFile and try to connect to server", "ConfigFile", config.Cfg.Kopia.ConfigFile, "URL", config.Cfg.Kopia.APIServer.RepositoryURL)
	if err := repo.ConnectAPIServer(k.Ctx, config.Cfg.Kopia.ConfigFile, &serverInfo, config.Cfg.Kopia.Password, &opts); err != nil {
		Logger.Error("Failed to generate configFile", "err", err, "ConfigFile", config.Cfg.Kopia.ConfigFile)
		return err
	}
	Logger.Debug("Successfully generated configFile", "ConfigFile", config.Cfg.Kopia.ConfigFile)
	return nil
}

func (k *KopiaClient) Connect() error {
	var err error

	if !config.Cfg.Kopia.ConnectWithConfigFile {
		if err := k.GenerateConfigFile(); err != nil {
			Logger.Error("Failed to launch repository server", "err", err, "ConfigFile", config.Cfg.Kopia.ConfigFile)
			k.IsConnected = false
			return err
		}
	}
	Logger.Debug("Try to connect to server", "ConfigFile", config.Cfg.Kopia.ConfigFile)
	k.Repo, err = repo.Open(k.Ctx, config.Cfg.Kopia.ConfigFile, config.Cfg.Kopia.Password, nil)
	if err != nil {
		Logger.Error("Failed to open repository", "err", err, "ConfigFile", config.Cfg.Kopia.ConfigFile)
		k.IsConnected = false
		return err
	}
	k.IsConnected = true
	return nil
}

func (k *KopiaClient) RunOnce() error {
	keepAllRetentions := (0 == len(config.Cfg.Kopia.Retentions))
	if !k.IsConnected {
		if err := k.Connect(); err != nil {
			return err
		}
	}
	// FIXME : if IsConnected == false, set error status to 1 (in metrics) and return

	// List all snapshot manifests (nil -> all sources)
	manifestsIds, err := snapshot.ListSnapshotManifests(k.Ctx, k.Repo, nil, nil)
	if err != nil {
		Logger.Error("failed to list snapshot manifests", "err", err, "ConfigFile", config.Cfg.Kopia.ConfigFile)
		return err
	}

	manifests, err := snapshot.LoadSnapshots(k.Ctx, k.Repo, manifestsIds)
	if err != nil {
		Logger.Error("failed to snapshot manifests", "err", err, "ConfigFile", config.Cfg.Kopia.ConfigFile)
		return err
	}

	for _, snapshotGroup := range snapshot.GroupBySource(manifests) {
		snapshotGroup = snapshot.SortByTime(snapshotGroup, true)
		src := snapshotGroup[0].Source

		// compute retention reason
		pol, _, _, err := policy.GetEffectivePolicy(k.Ctx, k.Repo, src)
		if err != nil {
			Logger.Error("Unable to determine effective policy", "Source", fmt.Sprintf("%v", src))
		} else {
			pol.RetentionPolicy.ComputeRetentionReasons(snapshotGroup)
		}

		// Iterate over snapshotGroup of manifests
		for _, m := range snapshotGroup {
			// fmt.Printf("ID: %v  Source: %v  Time: %v  Retentions: %v\n", m.ID, m.Source, m.StartTime, m.RetentionReasons)
			for _, rr := range m.RetentionReasons {
				if slices.Contains(config.Cfg.Kopia.Retentions, rr) || keepAllRetentions {
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
	repo.Disconnect(k.Ctx, config.Cfg.Kopia.ConfigFile)
	Logger.Debug("Disconnected from server", "ConfigFile", config.Cfg.Kopia.ConfigFile)
	k.IsConnected = false
}

func main() {
	k := NewKopiaClient()
	k.RunOnce()
}
