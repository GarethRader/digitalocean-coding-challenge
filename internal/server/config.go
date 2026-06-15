package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

var (
	serverPort      string
	certFilePath    string
	keyFilePath     string
	databaseURL     string
	cacheMaxEntries int
)

const defaultCacheMaxEntries = 10000

func Init() error {
	ENV, err := godotenv.Read(".env")
	if err != nil {
		return err
	}

	serverPort = ENV["PORT"]
	if serverPort == "" {
		serverPort = "8080"
	}

	databaseURL = strings.TrimSpace(ENV["DATABASE_URL"])
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is not configured")
	}

	cacheMaxEntries = defaultCacheMaxEntries
	if envValue := strings.TrimSpace(ENV["CACHE_MAX_ENTRIES"]); envValue != "" {
		parsed, err := strconv.Atoi(envValue)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("CACHE_MAX_ENTRIES must be a positive integer")
		}
		cacheMaxEntries = parsed
	}

	certFilePath = strings.TrimSpace(ENV["CERT_FILE_PATH"])
	keyFilePath = strings.TrimSpace(ENV["KEY_FILE_PATH"])

	if certFilePath != "" || keyFilePath != "" {
		if certFilePath == "" || keyFilePath == "" {
			return fmt.Errorf("both CERT_FILE_PATH and KEY_FILE_PATH must be configured to enable TLS")
		}

		if _, err := os.Stat(certFilePath); err != nil {
			return err
		}
		certFilePath, _ = filepath.Abs(certFilePath)

		if _, err := os.Stat(keyFilePath); err != nil {
			return err
		}
		keyFilePath, _ = filepath.Abs(keyFilePath)
	}

	return nil
}
