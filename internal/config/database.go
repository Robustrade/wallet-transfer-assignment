package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LoadEnvFile loads simple KEY=value lines without overwriting variables set by
// the caller's shell. A missing file is allowed so deployments need no local file.
func LoadEnvFile(path string) error {
	file, err := os.Open(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open env file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return fmt.Errorf("invalid env file line %d: expected KEY=value", lineNumber)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return fmt.Errorf("invalid quoted value on env file line %d", lineNumber)
			}
			value = unquoted
		} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set env variable from line %d: %w", lineNumber, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read env file: %w", err)
	}
	return nil
}

// DatabaseURL returns an explicit URL when provided, otherwise builds one from
// the individual PostgreSQL connection settings.
func DatabaseURL(explicitURL, host, port, user, password, database, sslMode string) (string, error) {
	if explicitURL = strings.TrimSpace(explicitURL); explicitURL != "" {
		return explicitURL, nil
	}
	if host == "" && port == "" && user == "" && password == "" && database == "" && sslMode == "" {
		return "", nil
	}
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "5432"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("DB_PORT must be a valid TCP port")
	}
	if user == "" || database == "" {
		return "", fmt.Errorf("DB_USER and DB_NAME are required when DATABASE_URL is not set")
	}
	if sslMode == "" {
		sslMode = "disable"
	}
	query := url.Values{}
	query.Set("sslmode", sslMode)
	connectionURL := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(host, strconv.Itoa(portNumber)),
		Path:     "/" + database,
		RawQuery: query.Encode(),
	}
	return connectionURL.String(), nil
}
