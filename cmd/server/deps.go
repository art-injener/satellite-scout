package main

import (
	"io/fs"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/satnogs"
)

// routeDeps — зависимости HTTP-слоя в setupRoutes.
type routeDeps struct {
	Cfg         *config.Config
	ConfigStore *config.Store
	Templates   fs.FS
	Static      fs.FS

	SSE      *handlers.SSEHub
	Tracking handlers.TrackingServiceInterface
	SatNOGS  *satnogs.Service

	Exclude   handlers.ExclusionAdder
	PassCache handlers.PassCacheInvalidator
	Group     handlers.GroupRefresher
}
