package config

import "os"

type Config struct {
	AppName     string
	HTTPPort    string
	DatabaseURL string
}

func Load() Config {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	return Config{
		AppName:     os.Getenv("APP_NAME"),
		HTTPPort:    port,
		DatabaseURL: databaseURL(),
	}
}

func databaseURL() string {
	if value := os.Getenv("DATABASE_URL"); value != "" {
		return value
	}
	host, port, user, password, name, sslmode := os.Getenv("DATABASE_HOST"), os.Getenv("DATABASE_PORT"), os.Getenv("DATABASE_USER"), os.Getenv("DATABASE_PASSWORD"), os.Getenv("DATABASE_NAME"), os.Getenv("DATABASE_SSLMODE")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "5432"
	}
	if user == "" {
		user = "postgres"
	}
	if name == "" {
		name = "wallet"
	}
	if sslmode == "" {
		sslmode = "disable"
	}
	return "postgres://" + user + ":" + password + "@" + host + ":" + port + "/" + name + "?sslmode=" + sslmode
}
