package server

import (
	"log/slog"
	"net/http"

	"server_v2/internal/config"
)

type clientRouteRegistrar interface {
	Register(mux *http.ServeMux)
}

type routeGroup struct {
	logger      *slog.Logger
	outputPorts config.AppPortsConfiguration
}

func NewHandler(
	logger *slog.Logger,
	outputPorts config.AppPortsConfiguration,
	clientHandler clientRouteRegistrar,
) http.Handler {
	group := routeGroup{
		logger:      logger,
		outputPorts: outputPorts,
	}

	mux := http.NewServeMux()
	group.registerDiscoveryRoutes(mux)
	if clientHandler != nil {
		clientHandler.Register(mux)
	}

	return mux
}
