package exporter

import (
	"fmt"
	"kopia-go-exporter/modconfig"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

type Exporter struct {
	Port    int
	Metrics KopiaMetrics
	Reg     *prometheus.Registry
}

func NewExporter() *Exporter {
	ex := new(Exporter)
	ex.Port = modconfig.Cfg.Exporter.Port

	ex.Reg = prometheus.NewRegistry()
	ex.Reg.MustRegister(collectors.NewGoCollector())
	ex.Reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	ex.SetBuildInfo()
	ex.RegisterKopiaMetrics()

	return ex
}

func (ex *Exporter) SetBuildInfo() {
	version, revision, time, _, ok := modconfig.GetVersionFull()
	if !ok {
		Logger.Error().Str("version", version).Msg("Failed to retrieve full version info; metric build_info will not be available")
	}

	// Create build_info gauge with labels for version, commit, date
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "build_info",
			Help:      "Build information",
		},
		[]string{"version", "commit", "date"},
	)

	ex.Reg.MustRegister(buildInfo)

	// Set build info with value 1
	buildInfo.WithLabelValues(version, revision, time).Set(1)
}

func (ex *Exporter) RegisterKopiaMetrics() {
	ex.Metrics.TotalSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "total_size",
			Help:      "Total size of the backup",
		},
		[]string{"host", "path", "user"},
	)
	ex.Reg.MustRegister(ex.Metrics.TotalSize)
	ex.Metrics.FileCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "file_count",
			Help:      "Number of files in the backup",
		},
		[]string{"host", "path", "user"},
	)
	ex.Reg.MustRegister(ex.Metrics.FileCount)
	ex.Metrics.DirCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "dir_count",
			Help:      "Number of directories in the backup",
		},
		[]string{"host", "path", "user"},
	)
	ex.Reg.MustRegister(ex.Metrics.DirCount)
	ex.Metrics.ErrorCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "error_count",
			Help:      "Number of errors in the backup",
		},
		[]string{"host", "path", "user"},
	)
	ex.Reg.MustRegister(ex.Metrics.ErrorCount)
	ex.Metrics.BackupDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_duration",
			Help:      "Duration of the backup",
		},
		[]string{"host", "path", "user"},
	)
	ex.Reg.MustRegister(ex.Metrics.BackupDuration)
	ex.Metrics.BackupStartTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_start_time",
			Help:      "Start time of the backup",
		},
		[]string{"host", "path", "user"},
	)
	ex.Reg.MustRegister(ex.Metrics.BackupStartTime)
	ex.Metrics.BackupEndTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_end_time",
			Help:      "End time of the backup",
		},
		[]string{"host", "path", "user"},
	)
	ex.Reg.MustRegister(ex.Metrics.BackupEndTime)
}

func (ex Exporter) Run() {
	// Start HTTP server exposing /metrics endpoint
	http.Handle("/metrics", promhttp.HandlerFor(ex.Reg, promhttp.HandlerOpts{}))
	http.ListenAndServe(fmt.Sprintf(":%d", ex.Port), nil)
	Logger.Debug().Int("port", ex.Port).Msg("Started http server")
}

func main() {
	exporter := NewExporter()

	go exporter.Run()
	select {} // block forever to keep main alive
}
