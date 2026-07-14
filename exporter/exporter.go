// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"kopia-go-exporter/config"
	"kopia-go-exporter/logger"
)

type Exporter struct {
	Port int
	Reg  *prometheus.Registry
}

func NewExporter() *Exporter {
	l := logger.Get()
	ex := new(Exporter)
	ex.Port = config.Cfg.Exporter.Port

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

func (ex Exporter) Run(ctx context.Context) {
	l := logger.Get()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(ex.Reg, promhttp.HandlerOpts{}))
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", ex.Port),
		Handler: mux,
	}
	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			l.Error("HTTP server shutdown error", "port", ex.Port, "err", err)
		}
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		l.Error("HTTP server error", "port", ex.Port, "err", err)
	}
}
