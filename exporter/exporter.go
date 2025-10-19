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

type Exporter struct {
	Port int
}

func NewExporter() *Exporter {
	ex := new(Exporter)
	ex.Port = modconfig.Cfg.Exporter.Port

	return ex
}

var Logger zerolog.Logger

func setBuildInfo(reg *prometheus.Registry) {
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

	reg.MustRegister(buildInfo)

	// Set build info with value 1
	buildInfo.WithLabelValues(version, revision, time).Set(1)
}

func registerKopiaMetrics(reg *prometheus.Registry) {
	reg.MustRegister(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "total_size",
			Help:      "Total size of the backup",
		},
		[]string{"host", "path", "user"},
	))
	reg.MustRegister(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "file_count",
			Help:      "Number of files in the backup",
		},
		[]string{"host", "path", "user"},
	))
	reg.MustRegister(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "dir_count",
			Help:      "Number of directories in the backup",
		},
		[]string{"host", "path", "user"},
	))
	reg.MustRegister(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "error_count",
			Help:      "Number of errors in the backup",
		},
		[]string{"host", "path", "user"},
	))
	reg.MustRegister(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_duration",
			Help:      "Duration of the backup",
		},
		[]string{"host", "path", "user"},
	))
	reg.MustRegister(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_start_time",
			Help:      "Start time of the backup",
		},
		[]string{"host", "path", "user"},
	))
	reg.MustRegister(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: modconfig.Cfg.Exporter.Metrics.Prefix,
			Name:      "backup_end_time",
			Help:      "End time of the backup",
		},
		[]string{"host", "path", "user"},
	))
}

func (ex Exporter) Run() {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	setBuildInfo(reg)
	registerKopiaMetrics(reg)

	// Start HTTP server exposing /metrics endpoint
	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	http.ListenAndServe(fmt.Sprintf(":%d", ex.Port), nil)
	Logger.Debug().Int("port", ex.Port).Msg("Started http server")
}

func main() {
	exporter := NewExporter()

	go exporter.Run()
	select {} // block forever to keep main alive
}
