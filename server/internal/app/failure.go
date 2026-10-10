package app

import (
	"errors"

	"blob/server/internal/platform/config"
	"blob/server/internal/platform/database"
)

func FailureCode(err error) string {
	switch {
	case errors.Is(err, config.ErrDatabaseURLRequired), errors.Is(err, database.ErrInvalidConfig):
		return "database_config_invalid"
	case errors.Is(err, database.ErrUnavailable):
		return "database_unavailable"
	case errors.Is(err, config.ErrIdentityConfigInvalid):
		return "identity_config_invalid"
	default:
		return "runtime_failed"
	}
}
