// Package pluginiface defines the Plugin interface and supporting types.
//
// These types mirror the interface from config-manager-core/internal/plugin.
// They are duplicated here so that this plugin can be built and tested
// independently without requiring access to the (private) core repository.
package pluginiface

import "net/http"

// Plugin is the interface that all Config Manager plugins must implement.
type Plugin interface {
	// Name returns the unique plugin identifier (e.g., "update", "network").
	Name() string

	// Version returns the plugin version string (semver recommended).
	Version() string

	// Description returns a human-readable description of the plugin.
	Description() string

	// Routes returns an http.Handler to be mounted under
	// /api/v1/plugins/{Name()}. Return nil if the plugin has no HTTP routes.
	Routes() http.Handler

	// ScheduledJobs returns job definitions for the scheduler.
	// Return nil or an empty slice if no scheduled jobs are needed.
	ScheduledJobs() []JobDefinition
}

// JobDefinition describes a scheduled job provided by a plugin.
type JobDefinition struct {
	// ID is globally unique, conventionally "{plugin_name}.{job_name}".
	ID          string
	Description string
	Cron        string       // cron expression, e.g. "0 3 * * *"
	Func        func() error // the function to execute
}
