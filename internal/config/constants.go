package config

const (
	// EnvHTTPAddr is the environment variable for the HTTP listen address.
	EnvHTTPAddr = "HTTP_ADDR"
	// EnvDatabaseURL is the environment variable for the PostgreSQL DSN. When
	// empty the server runs with the in-memory store, which is handy for local
	// exploration and demos.
	EnvDatabaseURL = "DATABASE_URL"
	// EnvDBMaxConns is the environment variable for the maximum number of
	// database connections in the pool.
	EnvDBMaxConns = "DB_MAX_CONNS"

	DefaultHTTPAddr = ":8080"
)
