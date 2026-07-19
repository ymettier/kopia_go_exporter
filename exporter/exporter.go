// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"kopia-go-exporter/config"
	"kopia-go-exporter/logger"
)

type Exporter struct {
	Port int
	Reg  *prometheus.Registry
	cfg  config.ExporterConfig
}

// NewExporter creates a new Exporter with a Prometheus registry and build info metric.
func NewExporter(cfg config.ExporterConfig) *Exporter {
	l := logger.Get()
	ex := new(Exporter)
	ex.cfg = cfg
	ex.Port = cfg.Port

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

// SetBuildInfo registers a build_info gauge with version, commit, and date labels.
func (ex *Exporter) SetBuildInfo(version, revision, buildTime string) {
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ex.cfg.Metrics.Prefix,
			Name:      "build_info",
			Help:      "Build information",
		},
		[]string{"version", "commit", "date"}, //nolint:goconst
	)

	ex.Reg.MustRegister(buildInfo)

	// Set build info with value 1
	buildInfo.WithLabelValues(version, revision, buildTime).Set(1)
}

// Run starts the HTTP server serving /metrics and blocks until ctx is canceled.
func (ex *Exporter) Run(ctx context.Context) {
	l := logger.Get()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(ex.Reg, promhttp.HandlerOpts{}))
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", ex.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		shutdownServer(srv, ex.Port)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		l.Error("HTTP server error", "port", ex.Port, "err", err)
	}
}

// Shutdowner is the subset of *http.Server used to gracefully stop the exporter.
type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

// shutdownServer gracefully shuts down the HTTP server, logging an error if it fails.
func shutdownServer(s Shutdowner, port int) {
	l := logger.Get()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:mnd
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		l.Error("HTTP server shutdown error", "port", port, "err", err)
	}
}
