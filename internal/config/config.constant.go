package config

// Application Constants
const (
	PortKey                  = "PORT"
	MaxConcurrentRequestsKey = "MAX_CONCURRENT_REQUESTS"
	LogLevelKey              = "LOG_LEVEL"
)

// Database Constants
const (
	usernameKey = "DB_USERNAME"
	passwordKey = "DB_PASSWORD"
	hostKey     = "DB_HOST"
	portKey     = "DB_PORT"
	dbNameKey   = "DB_NAME"

	DBMaxOpenConnsKey           = "DB_MAX_OPEN_CONNS"
	DBMaxIdleConnsKey           = "DB_MAX_IDLE_CONNS"
	DBConnMaxLifetimeMinutesKey = "DB_CONN_MAX_LIFETIME_MINUTES"
	DBConnMaxIdleTimeMinutesKey = "DB_CONN_MAX_IDLE_TIME_MINUTES"
)
