// Command nx-cache-server serves the Nx custom remote cache API backed by S3.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/snapaddy/nx-cache-server/internal/api"
	"github.com/snapaddy/nx-cache-server/internal/handler"
	s3store "github.com/snapaddy/nx-cache-server/internal/store/s3"
)

const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 2 * time.Minute
	shutdownTimeout   = 30 * time.Second
)

var logLevel = &slog.LevelVar{}

func main() {
	ctx := context.Background()

	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		slog.ErrorContext(ctx, "failed to run", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return fmt.Errorf("parse config: %w", err)
	}

	logLevel.Set(cfg.LogLevel)
	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := newS3Store(ctx, cfg)
	if err != nil {
		return err
	}

	srv, err := api.NewServer(
		handler.New(st, logger),
		handler.NewSecurity(cfg.NXAccessToken, cfg.ReadOnlyToken),
		api.WithErrorHandler(handler.NewErrorHandler(logger)),
	)
	if err != nil {
		return fmt.Errorf("create api server: %w", err)
	}

	mux := http.NewServeMux()
	// Unauthenticated liveness/readiness endpoint for Kubernetes probes,
	// outside the OpenAPI surface and request logging.
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/", handler.RequestLogger(logger, srv))

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		// Deliberately no ReadTimeout/WriteTimeout: artifact uploads and
		// downloads are long-lived streams of unbounded size.
	}

	errCh := make(chan error, 1)

	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()

	logger.InfoContext(ctx, "server listening", slog.Int("port", cfg.Port), slog.String("bucket", cfg.Bucket))

	select {
	case <-ctx.Done():
		logger.InfoContext(ctx, "shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}

		return nil
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	}
}

func newS3Store(ctx context.Context, cfg *serverConfig) (*s3store.Store, error) {
	var loadOpts []func(*awsconfig.LoadOptions) error
	if cfg.S3Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.S3Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}

		o.UsePathStyle = cfg.S3UsePathStyle
		// The SDK's default flexible checksums require TLS plus trailing
		// checksums for unseekable streams, which breaks streaming uploads
		// to plain-HTTP endpoints such as local MinIO. Artifacts are
		// content-addressed by their hash, so skip optional checksums.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		// Payload signing likewise requires a seekable body. Over TLS the
		// SDK already signs with UNSIGNED-PAYLOAD, so this only changes
		// behavior for plain-HTTP endpoints.
		o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
	})

	return s3store.New(client, cfg.Bucket, cfg.StoragePrefix), nil
}
