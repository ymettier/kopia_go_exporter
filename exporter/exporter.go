package exporter

import (
	"fmt"
	"kopia-go-exporter/modconfig"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
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

func (ex Exporter) SetBuildInfo() {
	version, revision, time, _, ok := modconfig.GetVersionFull()
	if !ok {
		Logger.Error().Str("version", version).Msg("Failed to retrieve full version info; metric build_info will not be available")
	}

	// Create build_info gauge with labels for version, commit, date
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "build_info",
			Help: "Build information",
		},
		[]string{"version", "commit", "date"},
	)
	prometheus.MustRegister(buildInfo)
	// Set build info with value 1
	buildInfo.WithLabelValues(version, revision, time).Set(1)
}

func (ex Exporter) Run() {
	// Start HTTP server exposing /metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", ex.Port), nil)
	Logger.Debug().Int("port", ex.Port).Msg("Started http server")
}

func main() {
	exporter := NewExporter()

	go exporter.Run()
	select {} // block forever to keep main alive
}
