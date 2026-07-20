package main

import (
	"errors"
	"flag"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func validArgs() []string {
	return []string{"-bucket", "test-bucket", "-access-token", "secret"}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(validArgs(), io.Discard)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}

	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}

	if cfg.Bucket != "test-bucket" {
		t.Errorf("Bucket = %q, want %q", cfg.Bucket, "test-bucket")
	}

	if cfg.StoragePrefix != "" || cfg.ReadOnlyToken != "" || cfg.S3Region != "" || cfg.S3Endpoint != "" {
		t.Errorf("optional fields not empty: %+v", cfg)
	}

	if cfg.S3UsePathStyle {
		t.Error("S3UsePathStyle = true, want false")
	}
}

func TestParseConfigFromEnv(t *testing.T) {
	t.Setenv("NX_CACHE_LOG_LEVEL", "debug")
	t.Setenv("NX_CACHE_PORT", "9090")
	t.Setenv("NX_CACHE_BUCKET", "env-bucket")
	t.Setenv("NX_CACHE_STORAGE_PREFIX", "nx")
	t.Setenv("NX_CACHE_ACCESS_TOKEN", "env-secret")
	t.Setenv("NX_CACHE_READ_ONLY_TOKEN", "env-ro")
	t.Setenv("NX_CACHE_S3_REGION", "eu-central-1")
	t.Setenv("NX_CACHE_S3_ENDPOINT", "http://localhost:9000")
	t.Setenv("NX_CACHE_S3_USE_PATH_STYLE", "true")

	cfg, err := parseConfig(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}

	want := &serverConfig{
		LogLevel:       slog.LevelDebug,
		Port:           9090,
		Bucket:         "env-bucket",
		StoragePrefix:  "nx",
		AccessToken:    "env-secret",
		ReadOnlyToken:  "env-ro",
		S3Region:       "eu-central-1",
		S3Endpoint:     "http://localhost:9000",
		S3UsePathStyle: true,
	}
	if *cfg != *want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestParseConfigFlagBeatsEnv(t *testing.T) {
	t.Setenv("NX_CACHE_PORT", "9090")
	t.Setenv("NX_CACHE_BUCKET", "env-bucket")
	t.Setenv("NX_CACHE_ACCESS_TOKEN", "env-secret")

	cfg, err := parseConfig([]string{"-port", "7070", "-bucket", "flag-bucket"}, io.Discard)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}

	if cfg.Port != 7070 {
		t.Errorf("Port = %d, want 7070 (flag over env)", cfg.Port)
	}

	if cfg.Bucket != "flag-bucket" {
		t.Errorf("Bucket = %q, want %q (flag over env)", cfg.Bucket, "flag-bucket")
	}

	if cfg.AccessToken != "env-secret" {
		t.Errorf("NXAccessToken = %q, want env value", cfg.AccessToken)
	}
}

func TestParseConfigValidation(t *testing.T) {
	tests := []struct {
		wantErr error
		name    string
		args    []string
	}{
		{
			name:    "missing bucket",
			args:    []string{"-access-token", "secret"},
			wantErr: errMissingBucket,
		},
		{
			name:    "missing access token",
			args:    []string{"-bucket", "b"},
			wantErr: errMissingAccessToken,
		},
		{
			name:    "invalid port",
			args:    append(validArgs(), "-port", "0"),
			wantErr: errInvalidPort,
		},
		{
			name:    "port too large",
			args:    append(validArgs(), "-port", "70000"),
			wantErr: errInvalidPort,
		},
		{
			name:    "tokens equal",
			args:    append(validArgs(), "-read-only-token", "secret"),
			wantErr: errTokensEqual,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseConfig(tt.args, io.Discard)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseConfigBadLogLevel(t *testing.T) {
	_, err := parseConfig(append(validArgs(), "-log-level", "verbose"), io.Discard)
	if err == nil {
		t.Error("err = nil, want log level parse error")
	}
}

func TestParseConfigBadPortEnv(t *testing.T) {
	t.Setenv("NX_CACHE_PORT", "not-a-number")

	_, err := parseConfig(validArgs(), io.Discard)
	if err == nil {
		t.Error("err = nil, want port parse error")
	}
}

func TestParseConfigBadBoolEnv(t *testing.T) {
	t.Setenv("NX_CACHE_S3_USE_PATH_STYLE", "maybe")

	_, err := parseConfig(validArgs(), io.Discard)
	if err == nil {
		t.Error("err = nil, want bool parse error")
	}
}

func TestParseConfigHelp(t *testing.T) {
	var buf strings.Builder

	_, err := parseConfig([]string{"-h"}, &buf)
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("err = %v, want flag.ErrHelp", err)
	}

	if !strings.Contains(buf.String(), "-bucket") {
		t.Error("usage output does not mention -bucket")
	}
}
