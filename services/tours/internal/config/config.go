// Package config declares what the tours service needs to run.
package config

import (
	"github.com/nicodanke/dizen-v2-backend/pkg/amqp"
	"github.com/nicodanke/dizen-v2-backend/pkg/cache"
	"github.com/nicodanke/dizen-v2-backend/pkg/config"
	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	"github.com/nicodanke/dizen-v2-backend/pkg/jwt"
	"github.com/nicodanke/dizen-v2-backend/pkg/outbox"
)

// Config is the whole configuration of the service.
//
// The embedded structs are what keep the env var names identical across services: a shared
// subsystem declares its own variables once, here they are only composed.
type Config struct {
	config.Base

	Database database.Config
	Cache    cache.Config
	AMQP     amqp.Config
	JWT      jwt.Config
	Outbox   outbox.WorkerConfig
}

// Load reads the configuration, failing with an explicit message if anything is missing.
func Load(envFiles ...string) (*Config, error) {
	return config.Load[Config](envFiles...)
}
