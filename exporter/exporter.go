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

type Exporter struct {
	Port int
	Reg  *prometheus.Registry
}

func NewExporter() *Exporter {
	ex := new(Exporter)
	ex.Port = modconfig.Cfg.Exporter.Port

	ex.Reg = prometheus.NewRegistry()
	ex.Reg.MustRegister(collectors.NewGoCollector())
	ex.Reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	version, revision, time, _, ok := modconfig.GetVersionFull()
	if !ok {
		Logger.Error().Str("version", version).Msg("Failed to retrieve full version info; metric build_info will not be available")
	} else {
		ex.SetBuildInfo(version, revision, time)
	}

	return ex
}

func (ex *Exporter) SetBuildInfo(version, revision, time string) {
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
