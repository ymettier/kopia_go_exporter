// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"kopia-go-exporter/internal/config"
	"kopia-go-exporter/internal/logger"
)

type Exporter struct {
	Reg *prometheus.Registry
	cfg config.ExporterConfig
}

// NewExporter creates a new Exporter with a Prometheus registry and build info metric.
func NewExporter(cfg config.ExporterConfig, version string) *Exporter {
	l := logger.Get()
	ex := new(Exporter)
	ex.cfg = cfg

	ex.Reg = prometheus.NewRegistry()
	ex.Reg.MustRegister(collectors.NewGoCollector())
	ex.Reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	vi := config.GetVersionInfo(version)
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
		[]string{"version", "commit", "date"},
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
	srv := ex.newHTTPServer(mux)

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		l.Error("HTTP server error", "port", ex.cfg.Port, "err", err)
		return
	}

	serveErr := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		serveErr <- srv.Serve(&readyListener{Listener: listener, ready: ready})
	}()
	select {
	case <-ready:
	case err = <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			l.Error("HTTP server error", "port", ex.cfg.Port, "err", err)
		}
		return
	}

	select {
	case <-ctx.Done():
		shutdownServer(srv, ex.cfg.Port)
		err = <-serveErr
	case err = <-serveErr:
	}
	if err != nil && err != http.ErrServerClosed {
		l.Error("HTTP server error", "port", ex.cfg.Port, "err", err)
	}
}

type readyListener struct {
	net.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *readyListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		close(l.ready)
	})
	return l.Listener.Accept()
}

// newHTTPServer creates an HTTP server for serving metrics with sensible timeouts.
func (ex *Exporter) newHTTPServer(mux *http.ServeMux) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", ex.cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
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
		if closer, ok := s.(interface{ Close() error }); ok {
			if closeErr := closer.Close(); closeErr != nil {
				l.Error("HTTP server close error", "port", port, "err", closeErr)
			}
		}
	}
}
