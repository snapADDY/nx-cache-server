package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

const envPrefix = "NX_CACHE_"

var (
	errMissingBucket      = errors.New("bucket is required (set -bucket or NX_CACHE_BUCKET)")
	errMissingAccessToken = errors.New("access token is required (set -access-token or NX_CACHE_ACCESS_TOKEN)")
	errInvalidPort        = errors.New("port must be between 1 and 65535")
	errTokensEqual        = errors.New("read-only token must differ from the access token")
)

type serverConfig struct {
	// Bucket is the name of the S3 bucket to use for storing cache files.
	Bucket string

	// StoragePrefix is the prefix to use for storing cache files in the S3 bucket.
	StoragePrefix string

	// AccessToken is the read-write access token for the NX API.
	AccessToken string

	// ReadOnlyToken optionally grants read-only access; writes with it return 403.
	ReadOnlyToken string

	// S3Region overrides the AWS SDK's default region resolution.
	S3Region string

	// S3Endpoint overrides the S3 endpoint, e.g. for MinIO or localstack.
	S3Endpoint string

	// LogLevel is the log level to use for the cache server.
	LogLevel slog.Level

	// Port is the port to listen on for the cache server.
	Port int

	// S3UsePathStyle forces path-style addressing, required by most
	// S3-compatible servers such as MinIO.
	S3UsePathStyle bool
}

// parseConfig resolves configuration with precedence: CLI flag > environment
// variable > default. Each flag maps to an env var derived from its name,
// e.g. -s3-region to NX_CACHE_S3_REGION.
func parseConfig(args []string, stderr io.Writer) (*serverConfig, error) {
	cfg := &serverConfig{}

	fs := flag.NewFlagSet("nx-cache-server", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.TextVar(&cfg.LogLevel, "log-level", slog.LevelInfo, "log level: debug, info, warn, error")
	fs.IntVar(&cfg.Port, "port", 8080, "port to listen on")
	fs.StringVar(&cfg.Bucket, "bucket", "", "S3 bucket for cache artifacts, required")
	fs.StringVar(&cfg.StoragePrefix, "storage-prefix", "", "key prefix inside the S3 bucket")
	fs.StringVar(&cfg.AccessToken, "access-token", "", "read-write bearer token, required; prefer the env var over the flag")
	fs.StringVar(&cfg.ReadOnlyToken, "read-only-token", "", "optional read-only bearer token; prefer the env var over the flag")
	fs.StringVar(&cfg.S3Region, "s3-region", "", "AWS region, falls back to the SDK default chain")
	fs.StringVar(&cfg.S3Endpoint, "s3-endpoint", "", "custom S3 endpoint, e.g. for MinIO")
	fs.BoolVar(&cfg.S3UsePathStyle, "s3-use-path-style", false, "use path-style S3 addressing, needed for MinIO")

	fs.VisitAll(func(f *flag.Flag) {
		f.Usage += fmt.Sprintf(" (env %s)", envName(f.Name))
	})

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parse flags: %w", err)
	}

	if err := applyEnv(fs); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// applyEnv sets every flag that was not given on the command line from its
// corresponding environment variable, using the flag's own value parser.
func applyEnv(fs *flag.FlagSet) error {
	setOnCommandLine := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		setOnCommandLine[f.Name] = true
	})

	var err error

	fs.VisitAll(func(f *flag.Flag) {
		if err != nil || setOnCommandLine[f.Name] {
			return
		}

		env := envName(f.Name)
		if value, ok := os.LookupEnv(env); ok {
			if setErr := fs.Set(f.Name, value); setErr != nil {
				err = fmt.Errorf("parse env %s: %w", env, setErr)
			}
		}
	})

	return err
}

// envName maps a flag name to its environment variable,
// e.g. "s3-use-path-style" to "NX_CACHE_S3_USE_PATH_STYLE".
func envName(flagName string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

func (c *serverConfig) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("%w: got %d", errInvalidPort, c.Port)
	}

	if c.Bucket == "" {
		return errMissingBucket
	}

	if c.AccessToken == "" {
		return errMissingAccessToken
	}

	if c.ReadOnlyToken != "" && c.ReadOnlyToken == c.AccessToken {
		return errTokensEqual
	}

	return nil
}
