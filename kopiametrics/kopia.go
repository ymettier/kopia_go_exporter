// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/content"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/prometheus/client_golang/prometheus"

	"kopia-go-exporter/config"
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
	ConfigFile  string
	tempDir     string
	Opts        repo.ConnectOptions
	ServerInfo  repo.APIServerInfo
	Repo        repo.Repository
	Metrics     KopiaMetrics
}

func NewKopiaClient() *KopiaClient {
	k := new(KopiaClient)

	tempDir, err := os.MkdirTemp("", "kopia-go-exporter-*")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp directory: %v", err))
	}
	k.tempDir = tempDir
	k.ConfigFile = filepath.Join(tempDir, "kopia.cfg")

	return k
}

func newGaugeVec(reg *prometheus.Registry, name, help string) *prometheus.GaugeVec {
	gv := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      name,
			Help:      help,
		},
		[]string{"host", "path", "user", "retention"},
	)
	reg.MustRegister(gv)
	return gv
}

func (k *KopiaClient) RegisterKopiaMetrics(reg *prometheus.Registry) {
	k.Metrics.TotalSize = newGaugeVec(reg, "total_size", "Total size of the backup")
	k.Metrics.FileCount = newGaugeVec(reg, "file_count", "Number of files in the backup")
	k.Metrics.DirCount = newGaugeVec(reg, "dir_count", "Number of directories in the backup")
	k.Metrics.ErrorCount = newGaugeVec(reg, "error_count", "Number of errors in the backup")
	k.Metrics.BackupDuration = newGaugeVec(reg, "backup_duration", "Duration of the backup")
	k.Metrics.BackupStartTime = newGaugeVec(reg, "backup_start_time", "Start time of the backup")
	k.Metrics.BackupEndTime = newGaugeVec(reg, "backup_end_time", "End time of the backup")
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
	Logger.Debug("Generate ConfigFile and try to connect to server", "ConfigFile", k.ConfigFile, "URL", config.Cfg.Kopia.APIServer.RepositoryURL)
	if err := repo.ConnectAPIServer(k.Ctx, k.ConfigFile, &serverInfo, config.Cfg.Kopia.Password, &opts); err != nil {
		Logger.Error("Failed to generate configFile", "err", err, "ConfigFile", k.ConfigFile)
		return err
	}
	Logger.Debug("Successfully generated configFile", "ConfigFile", k.ConfigFile)
	return nil
}

func (k *KopiaClient) Connect() error {
	var err error

	if err := k.GenerateConfigFile(); err != nil {
		Logger.Error("Failed to launch repository server", "err", err, "ConfigFile", k.ConfigFile)
		k.IsConnected = false
		return err
	}
	Logger.Debug("Try to connect to server", "ConfigFile", k.ConfigFile)
	k.Repo, err = repo.Open(k.Ctx, k.ConfigFile, config.Cfg.Kopia.Password, nil)
	if err != nil {
		Logger.Error("Failed to open repository", "err", err, "ConfigFile", k.ConfigFile)
		k.IsConnected = false
		return err
	}
	k.IsConnected = true
	return nil
}

func (k *KopiaClient) setSnapshotMetrics(m *snapshot.Manifest, keepAllRetentions bool) {
	for _, rr := range m.RetentionReasons {
		if !slices.Contains(config.Cfg.Kopia.Retentions, rr) && !keepAllRetentions {
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

func (k *KopiaClient) RunOnce() error {
	keepAllRetentions := len(config.Cfg.Kopia.Retentions) == 0
	if !k.IsConnected {
		if err := k.Connect(); err != nil {
			return err
		}
	}
	// FIXME : if IsConnected == false, set error status to 1 (in metrics) and return

	// List all snapshot manifests (nil -> all sources)
	manifestsIds, err := snapshot.ListSnapshotManifests(k.Ctx, k.Repo, nil, nil)
	if err != nil {
		Logger.Error("failed to list snapshot manifests", "err", err, "ConfigFile", k.ConfigFile)
		return err
	}

	manifests, err := snapshot.LoadSnapshots(k.Ctx, k.Repo, manifestsIds)
	if err != nil {
		Logger.Error("failed to snapshot manifests", "err", err, "ConfigFile", k.ConfigFile)
		return err
	}

	for _, snapshotGroup := range snapshot.GroupBySource(manifests) {
		snapshotGroup = snapshot.SortByTime(snapshotGroup, true)
		src := snapshotGroup[0].Source

		pol, _, _, err := policy.GetEffectivePolicy(k.Ctx, k.Repo, src)
		if err != nil {
			Logger.Error("Unable to determine effective policy", "Source", fmt.Sprintf("%v", src))
		} else {
			pol.RetentionPolicy.ComputeRetentionReasons(snapshotGroup)
		}

		for _, m := range snapshotGroup {
			k.setSnapshotMetrics(m, keepAllRetentions)
		}
	}
	return nil
}

func (k *KopiaClient) Disconnect() {
	if err := repo.Disconnect(k.Ctx, k.ConfigFile); err != nil {
		if Logger != nil {
			Logger.Error("Failed to disconnect from Kopia repository", "ConfigFile", k.ConfigFile, "err", err)
		}
	}
	if Logger != nil {
		Logger.Debug("Disconnected from server", "ConfigFile", k.ConfigFile)
	}
	k.IsConnected = false
	if k.tempDir != "" {
		_ = os.RemoveAll(k.tempDir)
		k.tempDir = ""
	}
}
