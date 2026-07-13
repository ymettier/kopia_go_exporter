package exporter

import (
	"fmt"
	"log/slog"
	"kopia-go-exporter/config"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Logger *slog.Logger

type Exporter struct {
	Port int
	Reg  *prometheus.Registry
}

func NewExporter() *Exporter {
	ex := new(Exporter)
	ex.Port = config.Cfg.Exporter.Port

	ex.Reg = prometheus.NewRegistry()
	ex.Reg.MustRegister(collectors.NewGoCollector())
	ex.Reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	vi := config.GetVersionInfo()
	if vi.Revision == "" {
		Logger.Error("Failed to retrieve full version info; metric build_info will not be available", "version", vi.Version)
	} else {
		ex.SetBuildInfo(vi.Version, vi.Revision, vi.Time)
	}

	return ex
}

func (ex *Exporter) SetBuildInfo(version, revision, time string) {
	// Create build_info gauge with labels for version, commit, date
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: config.Cfg.Exporter.Metrics.Prefix,
			Name:      "build_info",
			Help:      "Build information",
		},
		[]string{"version", "commit", "date"},
	)

	ex.Reg.MustRegister(buildInfo)

	// Set build info with value 1
	buildInfo.WithLabelValues(version, revision, time).Set(1)
}

func (ex Exporter) Run() {
	http.Handle("/metrics", promhttp.HandlerFor(ex.Reg, promhttp.HandlerOpts{}))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", ex.Port), nil); err != nil {
		Logger.Error("HTTP server error", "port", ex.Port, "err", err)
	}
}
