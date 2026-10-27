// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kopia-go-exporter/internal/config"
	"kopia-go-exporter/internal/logger"
)

// Constructs a new exporter with different build info states.
func TestNewExporter(t *testing.T) {
	tests := []struct {
		name      string
		buildInfo func() (*debug.BuildInfo, bool)
		port      int
		checkPort bool
	}{
		{
			name: "build info available",
			buildInfo: func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{
					GoVersion: "go1.27.3",
					Settings: []debug.BuildSetting{
						{Key: "vcs.revision", Value: "abc123"},
						{Key: "vcs.time", Value: "2025-01-15T10:00:00Z"},
					},
				}, true
			},
			port:      12346,
			checkPort: true,
		},
		{
			name: "build info unavailable",
			buildInfo: func() (*debug.BuildInfo, bool) {
				return nil, false
			},
			port:      12347,
			checkPort: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origReadBuildInfo := config.ReadBuildInfo
			defer func() { config.ReadBuildInfo = origReadBuildInfo }()

			logger.Reset(nil)
			if tt.buildInfo != nil {
				config.ReadBuildInfo = tt.buildInfo
			}

			cfg := config.ExporterConfig{
				Port:    tt.port,
				Metrics: struct{ Prefix string }{Prefix: "test_prefix"},
			}
			ex := NewExporter(cfg, "test")
			require.NotNil(t, ex)
			if tt.checkPort {
				assert.Equal(t, cfg.Port, ex.cfg.Port)
			}
			assert.NotNil(t, ex.Reg)
		})
	}
}

// Sets the build_info metric and expects it to be registered with the
// provided version/commit/date labels and a value of 1.
func TestExporter_SetBuildInfo(t *testing.T) {
	cfg := config.ExporterConfig{
		Metrics: struct{ Prefix string }{Prefix: "test_prefix"},
	}
	type fields struct {
		Reg *prometheus.Registry
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "test registry after setting build_info metric",
			fields: fields{
				Reg: prometheus.NewRegistry(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &Exporter{
				Reg: tt.fields.Reg,
				cfg: cfg,
			}
			labels := map[string]string{
				"version": "test_version",
				"commit":  "test_revision",
				"date":    "13:37",
			}
			ex.SetBuildInfo(labels["version"], labels["commit"], labels["date"])

			metrics, err := ex.Reg.Gather()
			require.NoError(t, err)

			familyMap := make(map[string]*prometheus.GaugeVec)
			for _, mFamily := range metrics {
				if mFamily.GetName() == "test_prefix_build_info" {
					familyMap[mFamily.GetName()] = nil
					for _, m := range mFamily.Metric {
						for _, label := range m.GetLabel() {
							switch label.GetName() {
							case "version":
								assert.Equal(t, labels["version"], label.GetValue())
							case "commit":
								assert.Equal(t, labels["commit"], label.GetValue())
							case "date":
								assert.Equal(t, labels["date"], label.GetValue())
							}
						}
						assert.Equal(t, float64(1), m.GetGauge().GetValue())
					}
				}
			}
			assert.Contains(t, familyMap, "test_prefix_build_info", "metric test_prefix_build_info was not found")
		})
	}
}

// Starts the exporter HTTP server on a free port and expects a 200 OK
// response from the /metrics endpoint.
func TestExporter_Run(t *testing.T) {
	t.Run("Test the exporter on a free port", func(t *testing.T) {
		logger.Reset(nil)
		port := freePort(t)
		ctx, cancel := context.WithCancel(context.Background())
		ex := Exporter{
			Reg: prometheus.NewRegistry(),
			cfg: config.ExporterConfig{Port: port},
		}

		go ex.Run(ctx)

		time.Sleep(200 * time.Millisecond)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:"+strconv.Itoa(port)+"/metrics", http.NoBody)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		cancel()
	})
}

// Starts the exporter on a port already bound by another listener and
// expects Run to return promptly with a server error.
func TestExporter_Run_AlreadyInUse(t *testing.T) {
	logger.Reset(nil)

	port := freePort(t)
	blocker, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.NoError(t, err)
	defer func() { _ = blocker.Close() }()

	ex := Exporter{
		Reg: prometheus.NewRegistry(),
		cfg: config.ExporterConfig{Port: port},
	}

	done := make(chan struct{})
	go func() {
		ex.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "Run() did not return after server error")
	}
}

// freePort returns a currently-free TCP port by binding to :0 and
// releasing it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

type fakeShutdowner struct {
	err             error
	called          bool
	captured        error
	capturedContext context.Context
	closeCalled     bool
}

func (f *fakeShutdowner) Shutdown(ctx context.Context) error {
	f.called = true
	f.captured = f.err
	f.capturedContext = ctx
	return f.err
}

func (f *fakeShutdowner) Close() error {
	f.closeCalled = true
	return nil
}

// Shuts down a fake server and expects Shutdown to be called, errors to
// be observed, and a 5s timeout context to be passed. A failed graceful
// shutdown also forces the server closed.
func TestShutdownServer(t *testing.T) {
	tests := []struct {
		name          string
		shutdownErr   error
		wantCalled    bool
		wantClose     bool
		wantErr       bool
		wantDeadline  bool
		deadlineDelta time.Duration
	}{
		{
			name:          "success",
			wantCalled:    true,
			wantClose:     false,
			wantErr:       false,
			wantDeadline:  true,
			deadlineDelta: 6 * time.Second,
		},
		{
			name:          "error is logged",
			shutdownErr:   errors.New("boom"), //nolint:err113
			wantCalled:    true,
			wantClose:     true,
			wantErr:       true,
			wantDeadline:  true,
			deadlineDelta: 6 * time.Second,
		},
		{
			name:          "context has timeout",
			wantCalled:    true,
			wantClose:     false,
			wantErr:       false,
			wantDeadline:  true,
			deadlineDelta: 6 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeShutdowner{err: tt.shutdownErr}
			shutdownServer(f, 9090)
			assert.Equal(t, tt.wantCalled, f.called, "Shutdown call state")
			assert.Equal(t, tt.wantClose, f.closeCalled, "Close call state")
			if tt.wantErr {
				assert.Error(t, f.captured)
			} else {
				assert.NoError(t, f.captured)
			}
			if tt.wantDeadline {
				require.NotNil(t, f.capturedContext, "context should be passed to Shutdown")
				deadline, ok := f.capturedContext.Deadline()
				assert.True(t, ok, "context should have a deadline")
				assert.WithinDuration(t, deadline, time.Now(), tt.deadlineDelta,
					"deadline should be approximately now + 5s")
			}
		})
	}
}

// Runs the exporter with an already-canceled context and expects the
// listener to be closed before Run returns.
func TestExporter_Run_AlreadyCanceled(t *testing.T) {
	logger.Reset(nil)
	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ex := Exporter{
		Reg: prometheus.NewRegistry(),
		cfg: config.ExporterConfig{Port: port},
	}

	done := make(chan struct{})
	go func() {
		ex.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "Run() did not return after context cancellation")
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.NoError(t, err, "listener should be closed when Run returns")
	require.NoError(t, listener.Close())
}

// Starts the exporter HTTP server, cancels the context, and expects
// Run to return within a reasonable time after graceful shutdown.
func TestExporter_Run_Shutdown(t *testing.T) {
	logger.Reset(nil)
	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	ex := Exporter{
		Reg: prometheus.NewRegistry(),
		cfg: config.ExporterConfig{Port: port},
	}

	done := make(chan struct{})
	go func() {
		ex.Run(ctx)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "Run() did not return after context cancellation")
	}
}

// Starts the exporter HTTP server on a free port and verifies that the server is configured
// with Read, Write, and Idle timeouts in addition to ReadHeaderTimeout.
func TestExporter_ServerTimeouts(t *testing.T) {
	logger.Reset(nil)
	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	ex := Exporter{
		Reg: prometheus.NewRegistry(),
		cfg: config.ExporterConfig{Port: port},
	}

	go ex.Run(ctx)

	mux := http.NewServeMux()
	srv := ex.newHTTPServer(mux)

	require.NotNil(t, srv)
	assert.Greater(t, srv.ReadTimeout, 10*time.Second)
	assert.Greater(t, srv.WriteTimeout, 10*time.Second)
	assert.Greater(t, srv.IdleTimeout, 10*time.Second)
	assert.Greater(t, srv.ReadHeaderTimeout, time.Second)

	cancel()
}
