// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"kopia-go-exporter/config"
)

type Exporter struct {
	Port   int
	Reg    *prometheus.Registry
	Logger *slog.Logger
}

func NewExporter(l *slog.Logger) *Exporter {
	ex := new(Exporter)
	ex.Port = config.Cfg.Exporter.Port
	ex.Logger = l

	ex.Reg = prometheus.NewRegistry()
	ex.Reg.MustRegister(collectors.NewGoCollector())
	ex.Reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	vi := config.GetVersionInfo()
	if vi.Revision == "" {
		l.Error("Failed to retrieve full version info; metric build_info will not be available", "version", vi.Version)
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
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(ex.Reg, promhttp.HandlerOpts{}))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", ex.Port), mux); err != nil {
		ex.Logger.Error("HTTP server error", "port", ex.Port, "err", err)
	}
}
