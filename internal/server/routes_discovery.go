package server

import (
	"net/http"

	"github.com/fxamacker/cbor/v2"
)

type discoveryResponse struct {
	TCPPort    int `cbor:"tcp_port" json:"tcp_port"`
	TCPTLSPort int `cbor:"tcp_tls_port" json:"tcp_tls_port"`
	HTTPPort   int `cbor:"http_port" json:"http_port"`
	HTTPSPort  int `cbor:"https_port" json:"https_port"`
	WSPort     int `cbor:"ws_port" json:"ws_port"`
	WSSPort    int `cbor:"wss_port" json:"wss_port"`
}

func (g routeGroup) registerDiscoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/discovery/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		payload, err := cbor.Marshal(discoveryResponse{
			TCPPort:    g.outputPorts.TCPPort,
			TCPTLSPort: g.outputPorts.TCPTLSPort,
			HTTPPort:   g.outputPorts.HTTPPort,
			HTTPSPort:  g.outputPorts.HTTPSPort,
			WSPort:     g.outputPorts.WSPort,
			WSSPort:    g.outputPorts.WSSPort,
		})
		if err != nil {
			g.logger.Error("failed to encode discovery response", "error", err)
			http.Error(w, "failed to encode discovery payload", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/cbor")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write(payload); writeErr != nil {
			g.logger.Warn("failed to write discovery response", "error", writeErr)
		}
	})
}
