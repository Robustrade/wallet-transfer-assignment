package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the process configuration. Secrets are not loaded from YAML.
type Config struct {
	Server   Server
	Database Database
	Log      Log
}

// Server is the HTTP listener configuration.
type Server struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	RequestTimeout    time.Duration
}

// Database is the PostgreSQL connection configuration.
type Database struct {
	Host                 string
	Port                 int
	Name                 string
	User                 string
	Password             string
	SSLMode              string
	URL                  string
	MaxConns             int32
	MinConns             int32
	MaxConnLifetime      time.Duration
	MaxConnIdleTime      time.Duration
	ConnectRetries       int
	ConnectRetryInterval time.Duration
	LockTimeout          time.Duration
	StatementTimeout     time.Duration
}

// Log is the structured logger configuration.
type Log struct {
	Level  string
	Format string
}

// DSN returns the PostgreSQL connection string. The password is included.
func (d Database) DSN() string {
	if d.URL != "" {
		return d.URL
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:   d.Name,
	}
	query := u.Query()
	query.Set("sslmode", d.SSLMode)
	u.RawQuery = query.Encode()
	return u.String()
}

// RedactedTarget describes the database without secrets, for startup logs.
func (d Database) RedactedTarget() string {
	if d.URL != "" {
		return "database url from DATABASE_URL"
	}
	return fmt.Sprintf("host=%s port=%d db=%s user=%s sslmode=%s", d.Host, d.Port, d.Name, d.User, d.SSLMode)
}

type fileConfig struct {
	Server struct {
		Address           string   `yaml:"address"`
		ReadHeaderTimeout duration `yaml:"readHeaderTimeout"`
		ReadTimeout       duration `yaml:"readTimeout"`
		WriteTimeout      duration `yaml:"writeTimeout"`
		IdleTimeout       duration `yaml:"idleTimeout"`
		ShutdownTimeout   duration `yaml:"shutdownTimeout"`
		RequestTimeout    duration `yaml:"requestTimeout"`
	} `yaml:"server"`
	Database struct {
		Host                 string   `yaml:"host"`
		Port                 int      `yaml:"port"`
		Name                 string   `yaml:"name"`
		User                 string   `yaml:"user"`
		Password             *string  `yaml:"password"`
		SSLMode              string   `yaml:"sslmode"`
		MaxConns             int32    `yaml:"maxConns"`
		MinConns             int32    `yaml:"minConns"`
		MaxConnLifetime      duration `yaml:"maxConnLifetime"`
		MaxConnIdleTime      duration `yaml:"maxConnIdleTime"`
		ConnectRetries       int      `yaml:"connectRetries"`
		ConnectRetryInterval duration `yaml:"connectRetryInterval"`
		LockTimeout          duration `yaml:"lockTimeout"`
		StatementTimeout     duration `yaml:"statementTimeout"`
	} `yaml:"database"`
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
}

type duration time.Duration

func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("duration must be a string such as 5s: %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}
	*d = duration(parsed)
	return nil
}

// Load reads the YAML file and applies environment overrides.
// path defaults to config.yaml. WALLET_DB_PASSWORD or DATABASE_URL is required.
func Load(path string) (Config, error) {
	if path == "" {
		path = "config.yaml"
	}
	// The path is the operator-selected config file, not request input.
	raw, err := os.ReadFile(path) //nolint:gosec // G304: config path is intentional
	if err != nil {
		return Config{}, fmt.Errorf("read config file %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	var file fileConfig
	if err := decoder.Decode(&file); err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w", path, err)
	}
	if file.Database.Password != nil {
		return Config{}, fmt.Errorf("config file %s must not contain database.password; set WALLET_DB_PASSWORD or DATABASE_URL", path)
	}

	cfg := Config{
		Server: Server{
			Address:           file.Server.Address,
			ReadHeaderTimeout: time.Duration(file.Server.ReadHeaderTimeout),
			ReadTimeout:       time.Duration(file.Server.ReadTimeout),
			WriteTimeout:      time.Duration(file.Server.WriteTimeout),
			IdleTimeout:       time.Duration(file.Server.IdleTimeout),
			ShutdownTimeout:   time.Duration(file.Server.ShutdownTimeout),
			RequestTimeout:    time.Duration(file.Server.RequestTimeout),
		},
		Database: Database{
			Host:                 file.Database.Host,
			Port:                 file.Database.Port,
			Name:                 file.Database.Name,
			User:                 file.Database.User,
			SSLMode:              file.Database.SSLMode,
			MaxConns:             file.Database.MaxConns,
			MinConns:             file.Database.MinConns,
			MaxConnLifetime:      time.Duration(file.Database.MaxConnLifetime),
			MaxConnIdleTime:      time.Duration(file.Database.MaxConnIdleTime),
			ConnectRetries:       file.Database.ConnectRetries,
			ConnectRetryInterval: time.Duration(file.Database.ConnectRetryInterval),
			LockTimeout:          time.Duration(file.Database.LockTimeout),
			StatementTimeout:     time.Duration(file.Database.StatementTimeout),
		},
		Log: Log{
			Level:  file.Log.Level,
			Format: file.Log.Format,
		},
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) error {
	if value := strings.TrimSpace(os.Getenv("WALLET_DB_HOST")); value != "" {
		cfg.Database.Host = value
	}
	if value := strings.TrimSpace(os.Getenv("WALLET_DB_PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("WALLET_DB_PORT %q is not an integer", value)
		}
		cfg.Database.Port = port
	}
	if value := strings.TrimSpace(os.Getenv("WALLET_DB_NAME")); value != "" {
		cfg.Database.Name = value
	}
	if value := strings.TrimSpace(os.Getenv("WALLET_DB_USER")); value != "" {
		cfg.Database.User = value
	}
	if value := strings.TrimSpace(os.Getenv("WALLET_DB_SSLMODE")); value != "" {
		cfg.Database.SSLMode = value
	}
	cfg.Database.Password = os.Getenv("WALLET_DB_PASSWORD")
	cfg.Database.URL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	return nil
}

func (cfg Config) validate() error {
	if err := cfg.validateServer(); err != nil {
		return err
	}
	if err := cfg.validateDatabase(); err != nil {
		return err
	}
	return cfg.validateLog()
}

func (cfg Config) validateServer() error {
	if strings.TrimSpace(cfg.Server.Address) == "" {
		return fmt.Errorf("server.address is required")
	}
	checks := []struct {
		name  string
		value time.Duration
	}{
		{name: "server.readHeaderTimeout", value: cfg.Server.ReadHeaderTimeout},
		{name: "server.readTimeout", value: cfg.Server.ReadTimeout},
		{name: "server.writeTimeout", value: cfg.Server.WriteTimeout},
		{name: "server.idleTimeout", value: cfg.Server.IdleTimeout},
		{name: "server.shutdownTimeout", value: cfg.Server.ShutdownTimeout},
		{name: "server.requestTimeout", value: cfg.Server.RequestTimeout},
	}
	for _, check := range checks {
		if err := requirePositive(check.name, check.value); err != nil {
			return err
		}
	}
	return nil
}

func (cfg Config) validateDatabase() error {
	if cfg.Database.URL == "" {
		if err := cfg.validateDatabaseTarget(); err != nil {
			return err
		}
	}
	if cfg.Database.MaxConns < 1 {
		return fmt.Errorf("database.maxConns must be at least 1")
	}
	if cfg.Database.MinConns < 0 || cfg.Database.MinConns > cfg.Database.MaxConns {
		return fmt.Errorf("database.minConns must be between 0 and database.maxConns")
	}
	if cfg.Database.ConnectRetries < 1 {
		return fmt.Errorf("database.connectRetries must be at least 1")
	}
	if err := requirePositive("database.maxConnLifetime", cfg.Database.MaxConnLifetime); err != nil {
		return err
	}
	if err := requirePositive("database.maxConnIdleTime", cfg.Database.MaxConnIdleTime); err != nil {
		return err
	}
	if err := requirePositive("database.connectRetryInterval", cfg.Database.ConnectRetryInterval); err != nil {
		return err
	}
	if err := requirePositive("database.lockTimeout", cfg.Database.LockTimeout); err != nil {
		return err
	}
	if err := requirePositive("database.statementTimeout", cfg.Database.StatementTimeout); err != nil {
		return err
	}
	return nil
}

func (cfg Config) validateDatabaseTarget() error {
	if strings.TrimSpace(cfg.Database.Host) == "" {
		return fmt.Errorf("database.host is required")
	}
	if cfg.Database.Port < 1 || cfg.Database.Port > 65535 {
		return fmt.Errorf("database.port must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Database.Name) == "" {
		return fmt.Errorf("database.name is required")
	}
	if strings.TrimSpace(cfg.Database.User) == "" {
		return fmt.Errorf("database.user is required")
	}
	if cfg.Database.Password == "" {
		return fmt.Errorf("database password is required: set WALLET_DB_PASSWORD")
	}
	switch cfg.Database.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("database.sslmode %q is not supported", cfg.Database.SSLMode)
	}
}

func (cfg Config) validateLog() error {
	switch strings.ToLower(cfg.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level %q is not supported", cfg.Log.Level)
	}
	switch strings.ToLower(cfg.Log.Format) {
	case "json", "text":
	default:
		return fmt.Errorf("log.format %q is not supported", cfg.Log.Format)
	}
	return nil
}

func requirePositive(name string, value time.Duration) error {
	if value <= 0 {
		return fmt.Errorf("%s must be greater than 0", name)
	}
	return nil
}
