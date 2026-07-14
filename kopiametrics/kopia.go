// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/content"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/prometheus/client_golang/prometheus"

	"kopia-go-exporter/config"
	"kopia-go-exporter/logger"
)

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
	ConfigFile  string
	tempDir     string
	repo        repo.Repository
	Metrics     KopiaMetrics
	cfg         config.Config
}

// NewKopiaClient creates a new KopiaClient with a temp directory for the config file.
func NewKopiaClient(cfg config.Config) (*KopiaClient, error) {
	k := new(KopiaClient)
	k.cfg = cfg

	tempDir, err := os.MkdirTemp("", "kopia-go-exporter-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	k.tempDir = tempDir
	k.ConfigFile = filepath.Join(tempDir, "kopia.cfg")

	return k, nil
}

func newGaugeVec(reg *prometheus.Registry, namespace, name, help string) *prometheus.GaugeVec {
	gv := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      name,
			Help:      help,
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(gv)
	return gv
}

// RegisterKopiaMetrics registers all Prometheus gauge vectors with the given registry.
func (k *KopiaClient) RegisterKopiaMetrics(reg *prometheus.Registry) {
	prefix := k.cfg.Exporter.Metrics.Prefix
	k.Metrics.TotalSize = newGaugeVec(reg, prefix, "total_size", "Total size of the backup")
	k.Metrics.FileCount = newGaugeVec(reg, prefix, "file_count", "Number of files in the backup")
	k.Metrics.DirCount = newGaugeVec(reg, prefix, "dir_count", "Number of directories in the backup")
	k.Metrics.ErrorCount = newGaugeVec(reg, prefix, "error_count", "Number of errors in the backup")
	k.Metrics.BackupDuration = newGaugeVec(reg, prefix, "backup_duration", "Duration of the backup")
	k.Metrics.BackupStartTime = newGaugeVec(reg, prefix, "backup_start_time", "Start time of the backup")
	k.Metrics.BackupEndTime = newGaugeVec(reg, prefix, "backup_end_time", "End time of the backup")
}

// GenerateConfigFile connects to the Kopia API server and writes a config file to the temp directory.
func (k *KopiaClient) GenerateConfigFile() error {
	l := logger.Get()
	k.Ctx = context.Background()

	opts := repo.ConnectOptions{
		ClientOptions: repo.ClientOptions{
			Username: k.cfg.Kopia.APIServer.Username,
			Hostname: k.cfg.Kopia.APIServer.Hostname,
		},
		CachingOptions: content.CachingOptions{},
	}
	serverInfo := repo.APIServerInfo{
		BaseURL:                             k.cfg.Kopia.APIServer.RepositoryURL,
		TrustedServerCertificateFingerprint: k.cfg.Kopia.APIServer.Fingerprint,
	}

	l.Debug("Generate ConfigFile and try to connect to server", "ConfigFile", k.ConfigFile, "URL", k.cfg.Kopia.APIServer.RepositoryURL)
	if err := repo.ConnectAPIServer(k.Ctx, k.ConfigFile, &serverInfo, k.cfg.Kopia.Password, &opts); err != nil {
		l.Error("Failed to generate configFile", "err", err, "ConfigFile", k.ConfigFile)
		return err
	}
	l.Debug("Successfully generated configFile", "ConfigFile", k.ConfigFile)
	return nil
}

// Connect generates a config file and opens the Kopia repository.
func (k *KopiaClient) Connect() error {
	l := logger.Get()
	var err error

	if err := k.GenerateConfigFile(); err != nil {
		l.Error("Failed to launch repository server", "err", err, "ConfigFile", k.ConfigFile)
		k.IsConnected = false
		return err
	}
	l.Debug("Try to connect to server", "ConfigFile", k.ConfigFile)
	k.repo, err = repo.Open(k.Ctx, k.ConfigFile, k.cfg.Kopia.Password, nil)
	if err != nil {
		l.Error("Failed to open repository", "err", err, "ConfigFile", k.ConfigFile)
		k.IsConnected = false
		return err
	}
	k.IsConnected = true
	return nil
}

func (k *KopiaClient) setSnapshotMetrics(m *snapshot.Manifest, keepAllRetentions bool) {
	for _, rr := range m.RetentionReasons {
		if !slices.Contains(k.cfg.Kopia.Retentions, rr) && !keepAllRetentions {
			continue
		}
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

// RunOnce performs a single metrics collection cycle: connects, lists snapshots, and updates gauges.
func (k *KopiaClient) RunOnce() error {
	l := logger.Get()
	keepAllRetentions := len(k.cfg.Kopia.Retentions) == 0
	if !k.IsConnected {
		if err := k.Connect(); err != nil {
			return err
		}
	}
	// FIXME : if IsConnected == false, set error status to 1 (in metrics) and return

	// List all snapshot manifests (nil -> all sources)
	manifestsIds, err := snapshot.ListSnapshotManifests(k.Ctx, k.repo, nil, nil)
	if err != nil {
		l.Error("failed to list snapshot manifests", "err", err, "ConfigFile", k.ConfigFile)
		return err
	}

	manifests, err := snapshot.LoadSnapshots(k.Ctx, k.repo, manifestsIds)
	if err != nil {
		l.Error("failed to snapshot manifests", "err", err, "ConfigFile", k.ConfigFile)
		return err
	}

	for _, snapshotGroup := range snapshot.GroupBySource(manifests) {
		snapshotGroup = snapshot.SortByTime(snapshotGroup, true)
		src := snapshotGroup[0].Source

		pol, _, _, err := policy.GetEffectivePolicy(k.Ctx, k.repo, src)
		if err != nil {
			l.Error("Unable to determine effective policy", "Source", fmt.Sprintf("%v", src))
		} else {
			pol.RetentionPolicy.ComputeRetentionReasons(snapshotGroup)
		}

		for _, m := range snapshotGroup {
			k.setSnapshotMetrics(m, keepAllRetentions)
		}
	}
	return nil
}

// Disconnect closes the repository connection and removes the temp directory.
func (k *KopiaClient) Disconnect() {
	l := logger.Get()
	if err := repo.Disconnect(k.Ctx, k.ConfigFile); err != nil {
		l.Debug("Failed to disconnect from Kopia repository", "ConfigFile", k.ConfigFile, "err", err)
	}
	l.Debug("Disconnected from server", "ConfigFile", k.ConfigFile)
	k.IsConnected = false
	if k.tempDir != "" {
		_ = os.RemoveAll(k.tempDir)
		k.tempDir = ""
	}
}
