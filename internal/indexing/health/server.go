package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server provides HTTP endpoints for health monitoring.
type Server struct {
	monitor *Monitor
	server  *http.Server
}

// NewServer creates a new health server.
func NewServer(monitor *Monitor, port int) *Server {
	mux := http.NewServeMux()
	s := &Server{
		monitor: monitor,
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/health/detailed", s.handleDetailed)
	mux.Handle("/metrics", promhttp.Handler())

	return s
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

// Stop stops the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	report := s.monitor.CheckHealth(r.Context())
	status := StatusHealthy

	// Aggregate status (worst case wins)
	for _, chain := range report {
		if chain.Status == StatusCritical {
			status = StatusCritical
			break
		}
		if chain.Status == StatusDegraded {
			status = StatusDegraded
		}
	}

	response := map[string]string{"status": string(status)}
	w.Header().Set("Content-Type", "application/json")

	if status == StatusCritical {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleDetailed(w http.ResponseWriter, r *http.Request) {
	report := s.monitor.CheckHealth(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}
