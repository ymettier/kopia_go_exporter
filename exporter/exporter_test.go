package exporter

import (
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewExporter(t *testing.T) {
	tests := []struct {
		name string
		want *Exporter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewExporter(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewExporter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExporter_SetBuildInfo(t *testing.T) {
	type fields struct {
		Port int
		Reg  *prometheus.Registry
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "test registry after setting build_info metric",
			fields: fields{
				Port: 9090,
				Reg:  prometheus.NewRegistry(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &Exporter{
				Port: tt.fields.Port,
				Reg:  tt.fields.Reg,
			}
			labels := map[string]string{
				"version": "test_version",
				"commit":  "test_revision",
				"date":    "13:37",
			}
			ex.SetBuildInfo(labels["version"], labels["commit"], labels["date"])

			// Gather all metrics from the registry
			metrics, err := ex.Reg.Gather()
			if err != nil {
				t.Fatalf("error gathering metrics: %v", err)
			}

			found := false
			foundVersion := false
			foundRevision := false
			foundDate := false
			for _, mFamily := range metrics {
				if mFamily.GetName() == "build_info" {
					found = true
					for _, m := range mFamily.Metric {
						for _, label := range m.Label {
							labelName := label.GetName()
							labelValue := label.GetValue()
							if labelName == "version" {
								foundVersion = true
								if labelValue != labels[labelName] {
									t.Errorf("Found metric build_info and label %v, but got %v, wanted %v", labelName, labelValue, labels[labelName])
								}
							}
							if labelName == "commit" {
								foundRevision = true
								if labelValue != labels[labelName] {
									t.Errorf("Found metric build_info and label %v, but got %v, wanted %v", labelName, labelValue, labels[labelName])
								}
							}
							if labelName == "date" {
								foundDate = true
								if labelValue != labels[labelName] {
									t.Errorf("Found metric build_info and label %v, but got %v, wanted %v", labelName, labelValue, labels[labelName])
								}
							}
						}
						value := m.Gauge.GetValue()
						if value != 1 {
							t.Errorf("Found metric build_info but got value %v, wanted %v", value, 1)
						}
					}
				}
			}
			if !found {
				t.Errorf("Metric build_info was not found")
			} else {
				if !foundVersion {
					t.Errorf("Found metric build_info but label %v is missing", "version")
				}
				if !foundRevision {
					t.Errorf("Found metric build_info but label %v is missing", "commit")
				}
				if !foundDate {
					t.Errorf("Found metric build_info but label %v is missing", "date")
				}
			}
		})
	}
}

func TestExporter_Run(t *testing.T) {
	t.Run("Test the exporter on port 12345", func(t *testing.T) {
		ex := Exporter{
			Port: 12345,
			Reg:  prometheus.NewRegistry(),
		}
		Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		go ex.Run()

		// Wait briefly for the server to start
		time.Sleep(200 * time.Millisecond)

		resp, err := http.Get("http://localhost:12345/metrics")
		if err != nil {
			t.Fatalf("could not connect to exporter: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status code: got %v, want %v", resp.StatusCode, http.StatusOK)
		}
	})
}
