// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/content"
	"github.com/kopia/kopia/repo/manifest"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/prometheus/client_golang/prometheus"

	"kopia-go-exporter/config"
	"kopia-go-exporter/logger"
)

// openRepo is a test hook for repo.Open. In production it uses the real repo.Open.
// In tests, it can be replaced to simulate errors.
var openRepo func(context.Context, string, string, *repo.Options) (repo.Repository, error) = repo.Open

// loadSnapshotsFunc is a test hook for snapshot.LoadSnapshots. In production it uses
// the real function. In tests, it can be replaced to simulate errors.
var loadSnapshotsFunc func(context.Context, repo.Repository, []manifest.ID) ([]*snapshot.Manifest, error) = snapshot.LoadSnapshots

// getEffectivePolicyFunc is a test hook for policy.GetEffectivePolicy. In production
// it uses the real function. In tests, it can be replaced to simulate errors.
var getEffectivePolicyFunc = policy.GetEffectivePolicy

// filterCacheTTL is the lifetime of cached path-filter results. The cache is
// invalidated after this duration to drop entries that are no longer used.
const filterCacheTTL = 86400 * time.Second

type filterCacheEntry struct {
	result    bool
	expiresAt time.Time
}

var (
	filterCacheMu sync.Mutex
	filterCache   = make(map[string]filterCacheEntry)
	filterCacheTS = time.Now()
)

// matchPathFiltersCached returns whether the given path matches the filters,
// using a process-wide cache keyed by path to avoid re-running the regexes on
// every snapshot. The whole cache is invalidated every filterCacheTTL seconds.
func matchPathFiltersCached(path string, include, exclude []*regexp.Regexp) bool {
	now := time.Now()
	filterCacheMu.Lock()
	if now.Sub(filterCacheTS) >= filterCacheTTL {
		filterCache = make(map[string]filterCacheEntry)
		filterCacheTS = now
	}
	if entry, ok := filterCache[path]; ok && now.Before(entry.expiresAt) {
		filterCacheMu.Unlock()
		return entry.result
	}
	filterCacheMu.Unlock()

	result := matchPathFilters(path, include, exclude)

	filterCacheMu.Lock()
	filterCache[path] = filterCacheEntry{result: result, expiresAt: now.Add(filterCacheTTL)}
	filterCacheMu.Unlock()
	return result
}

type KopiaMetrics struct {
	TotalSize       *prometheus.GaugeVec
	FileCount       *prometheus.GaugeVec
	DirCount        *prometheus.GaugeVec
	ErrorCount      *prometheus.GaugeVec
	BackupDuration  *prometheus.GaugeVec
	BackupStartTime *prometheus.GaugeVec
	BackupEndTime   *prometheus.GaugeVec
	Up              prometheus.Gauge
}

type KopiaClient struct {
	isConnected bool
	configFile  string
	tempDir     string
	repo        repo.Repository
	metrics     KopiaMetrics
	cfg         *config.Config
}

// NewKopiaClient creates a new KopiaClient with a temp directory for the config file.
func NewKopiaClient(cfg *config.Config) (*KopiaClient, error) {
	k := new(KopiaClient)
	k.cfg = cfg

	tempDir, err := os.MkdirTemp("", "kopia-go-exporter-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	k.tempDir = tempDir
	k.configFile = filepath.Join(tempDir, "kopia.cfg")

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
	k.metrics.TotalSize = newGaugeVec(reg, prefix, "total_size", "Total size of the backup")
	k.metrics.FileCount = newGaugeVec(reg, prefix, "file_count", "Number of files in the backup")
	k.metrics.DirCount = newGaugeVec(reg, prefix, "dir_count", "Number of directories in the backup")
	k.metrics.ErrorCount = newGaugeVec(reg, prefix, "error_count", "Number of errors in the backup")
	k.metrics.BackupDuration = newGaugeVec(reg, prefix, "backup_duration", "Duration of the backup")
	k.metrics.BackupStartTime = newGaugeVec(reg, prefix, "backup_start_time", "Start time of the backup")
	k.metrics.BackupEndTime = newGaugeVec(reg, prefix, "backup_end_time", "End time of the backup")
	k.metrics.Up = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: prefix,
		Name:      "up",
		Help:      "Connection status to Kopia repository (1 = connected, 0 = disconnected)",
	})
	reg.MustRegister(k.metrics.Up)
}

// GenerateConfigFile connects to the Kopia API server and writes a config file to the temp directory.
func (k *KopiaClient) GenerateConfigFile(ctx context.Context) error {
	l := logger.Get()
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

	l.Debug("Generate ConfigFile and try to connect to server", "ConfigFile", k.configFile, "URL", k.cfg.Kopia.APIServer.RepositoryURL)
	if err := repo.ConnectAPIServer(ctx, k.configFile, &serverInfo, k.cfg.Kopia.Password, &opts); err != nil {
		l.Error("Failed to generate configFile", "err", err, "ConfigFile", k.configFile)
		return err
	}
	l.Debug("Successfully generated configFile", "ConfigFile", k.configFile)
	return nil
}

// Connect generates a config file and opens the Kopia repository.
func (k *KopiaClient) Connect(ctx context.Context) error {
	l := logger.Get()
	var err error

	if err := k.GenerateConfigFile(ctx); err != nil {
		l.Error("Failed to launch repository server", "err", err, "ConfigFile", k.configFile)
		k.isConnected = false
		return err
	}
	l.Debug("Try to connect to server", "ConfigFile", k.configFile)
	k.repo, err = openRepo(ctx, k.configFile, k.cfg.Kopia.Password, nil)
	if err != nil {
		l.Error("Failed to open repository", "err", err, "ConfigFile", k.configFile)
		k.isConnected = false
		return err
	}
	k.isConnected = true
	return nil
}

// matchPathFilters decides whether a snapshot source path should produce metrics.
// Exclude filters are applied first: if any exclude regex matches, the path is
// rejected. Then include filters are applied: if at least one include regex
// matches, the path is accepted. When there are no include filters, every path
// that was not excluded is accepted.
func matchPathFilters(path string, include, exclude []*regexp.Regexp) bool {
	matches := true
	for _, re := range exclude {
		if re.MatchString(path) {
			matches = false
			break
		}
	}
	if len(include) == 0 {
		return matches
	}
	for _, re := range include {
		if re.MatchString(path) {
			return true
		}
	}
	return matches
}

func (k *KopiaClient) setSnapshotMetrics(m *snapshot.Manifest, keepAllRetentions bool) {
	if !matchPathFiltersCached(m.Source.Path, k.cfg.Filters.Include.PathRegex, k.cfg.Filters.Exclude.PathRegex) {
		return
	}
	for _, rr := range m.RetentionReasons {
		if !slices.Contains(k.cfg.Kopia.Retentions, rr) && !keepAllRetentions {
			continue
		}
		labels := prometheus.Labels{"host": m.Source.Host, "path": m.Source.Path, "user": m.Source.UserName, "retention": rr}
		k.metrics.BackupStartTime.With(labels).Set(float64(m.StartTime.ToTime().Unix()))
		k.metrics.BackupEndTime.With(labels).Set(float64(m.EndTime.ToTime().Unix()))
		k.metrics.BackupDuration.With(labels).Set(max(0.0, float64(m.EndTime-m.StartTime)/1e9)) //nolint:mnd
		k.metrics.DirCount.With(labels).Set(float64(m.Stats.TotalDirectoryCount))
		k.metrics.ErrorCount.With(labels).Set(float64(m.Stats.ErrorCount))
		k.metrics.FileCount.With(labels).Set(float64(m.Stats.TotalFileCount))
		k.metrics.TotalSize.With(labels).Set(float64(m.Stats.TotalFileSize))
	}
}

// RunOnce performs a single metrics collection cycle: connects, lists snapshots, and updates gauges.
func (k *KopiaClient) RunOnce(ctx context.Context) error {
	l := logger.Get()
	keepAllRetentions := len(k.cfg.Kopia.Retentions) == 0
	if !k.isConnected {
		if err := k.Connect(ctx); err != nil {
			k.metrics.Up.Set(0)
			return err
		}
	}

	// List all snapshot manifests (nil -> all sources)
	manifestsIds, err := snapshot.ListSnapshotManifests(ctx, k.repo, nil, nil)
	if err != nil {
		l.Error("failed to list snapshot manifests", "err", err, "ConfigFile", k.configFile)
		k.isConnected = false
		k.metrics.Up.Set(0)
		return err
	}

	manifests, err := loadSnapshotsFunc(ctx, k.repo, manifestsIds)
	if err != nil {
		l.Error("failed to load snapshot manifests", "err", err, "ConfigFile", k.configFile)
		k.isConnected = false
		k.metrics.Up.Set(0)
		return err
	}

	k.metrics.Up.Set(1)

	for _, snapshotGroup := range snapshot.GroupBySource(manifests) {
		snapshotGroup = snapshot.SortByTime(snapshotGroup, true)
		src := snapshotGroup[0].Source

		pol, _, _, err := getEffectivePolicyFunc(ctx, k.repo, src)
		if err != nil {
			l.Error("Unable to determine effective policy", "err", err, "Source", src)
			continue
		}
		pol.RetentionPolicy.ComputeRetentionReasons(snapshotGroup)

		for _, m := range snapshotGroup {
			k.setSnapshotMetrics(m, keepAllRetentions)
		}
	}
	return nil
}

// Disconnect closes the repository connection and removes the temp directory.
func (k *KopiaClient) Disconnect(ctx context.Context) {
	l := logger.Get()
	if err := repo.Disconnect(ctx, k.configFile); err != nil {
		l.Debug("Failed to disconnect from Kopia repository", "ConfigFile", k.configFile, "err", err)
	}
	l.Debug("Disconnected from server", "ConfigFile", k.configFile)
	k.isConnected = false
	if k.tempDir != "" {
		if err := os.RemoveAll(k.tempDir); err != nil {
			l.Debug("Failed to remove temporary directory", "tempDir", k.tempDir, "err", err)
		}
		k.tempDir = ""
	}
}
