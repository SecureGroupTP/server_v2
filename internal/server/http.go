package server

import (
	"log/slog"
	"net/http"

	"server_v2/internal/config"
)

type routeGroup struct {
	logger      *slog.Logger
	outputPorts config.AppPortsConfiguration
}

func NewHandler(logger *slog.Logger, outputPorts config.AppPortsConfiguration) http.Handler {
	group := routeGroup{
		logger:      logger,
		outputPorts: outputPorts,
	}

	mux := http.NewServeMux()
	group.registerDiscoveryRoutes(mux)
	group.registerWebSocketRoutes(mux)

	return mux
}
