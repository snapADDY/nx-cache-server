# nx-cache-server

A self-hosted [Nx remote cache](https://nx.dev/recipes/running-tasks/self-hosted-caching) server backed by S3 (or any S3-compatible storage such as MinIO). Implements the official Nx custom remote cache OpenAPI specification: task outputs are uploaded once and shared across CI agents and developer machines.

- Streams artifacts between client and S3 without buffering them in memory
- Atomic overwrite protection via S3 conditional writes (`If-None-Match: "*"`)
- Bearer-token auth with an optional read-only token for consumers that must not write
- Structured JSON logging (slog), graceful shutdown on SIGINT/SIGTERM

## Minimal usage

```sh
docker run -p 8080:8080 \
  -e AWS_ACCESS_KEY_ID=... \
  -e AWS_SECRET_ACCESS_KEY=... \
  -e NX_CACHE_BUCKET=my-nx-cache-bucket \
  -e NX_CACHE_ACCESS_TOKEN=change-me \
  nx-cache-server
```

Or run the binary directly:

```sh
NX_CACHE_ACCESS_TOKEN=change-me ./nx-cache-server -bucket my-nx-cache-bucket
```

Point an Nx workspace at the server:

```sh
export NX_SELF_HOSTED_REMOTE_CACHE_SERVER=http://localhost:8080
export NX_SELF_HOSTED_REMOTE_CACHE_ACCESS_TOKEN=change-me
```

AWS credentials are resolved through the SDK's default chain (environment variables, shared config/credentials files, IAM roles / IRSA). The credentials need `s3:GetObject` and `s3:PutObject` on the bucket.

## Configuration

Every option can be set as a CLI flag or an environment variable. Precedence: **CLI flag > environment variable > default**.

| Flag                 | Environment variable         | Default | Required | Description                                                                                                              |
| -------------------- | ---------------------------- | ------- | -------- | ------------------------------------------------------------------------------------------------------------------------ |
| `-bucket`            | `NX_CACHE_BUCKET`            | —       | yes      | S3 bucket for cache artifacts                                                                                            |
| `-access-token`      | `NX_CACHE_ACCESS_TOKEN`      | —       | yes      | Read-write bearer token. Prefer the env var: flag values are visible in the process list                                 |
| `-read-only-token`   | `NX_CACHE_READ_ONLY_TOKEN`   | —       | no       | Optional read-only bearer token. Downloads are allowed, uploads are rejected with 403. Must differ from the access token |
| `-port`              | `NX_CACHE_PORT`              | `8080`  | no       | Port to listen on                                                                                                        |
| `-log-level`         | `NX_CACHE_LOG_LEVEL`         | `info`  | no       | Log level: `debug`, `info`, `warn`, `error`                                                                              |
| `-storage-prefix`    | `NX_CACHE_STORAGE_PREFIX`    | —       | no       | Key prefix inside the bucket, e.g. `nx-cache`                                                                            |
| `-s3-region`         | `NX_CACHE_S3_REGION`         | —       | no       | AWS region; falls back to the SDK default chain (`AWS_REGION`, profile, IMDS)                                            |
| `-s3-endpoint`       | `NX_CACHE_S3_ENDPOINT`       | —       | no       | Custom S3 endpoint URL, e.g. `http://localhost:9000` for alternative S3 services                                         |
| `-s3-use-path-style` | `NX_CACHE_S3_USE_PATH_STYLE` | `false` | no       | Use path-style addressing (`endpoint/bucket/key`)                                                                        |

Run `nx-cache-server -h` for the same list from the binary.

### S3-compatible storage

Overwrite protection relies on conditional `PutObject` (`If-None-Match: "*"`). AWS S3 supports this natively
Example against local MinIO:

```sh
AWS_ACCESS_KEY_ID=minio AWS_SECRET_ACCESS_KEY=minio123 \
NX_CACHE_BUCKET=nx-cache NX_CACHE_ACCESS_TOKEN=change-me \
./nx-cache-server -s3-endpoint http://localhost:9000 -s3-use-path-style -s3-region us-east-1
```

## API

The server implements the Nx custom remote cache spec (`openapi.json`):

| Method                 | Path                   | Responses                                                                           |
| ---------------------- | ---------------------- | ----------------------------------------------------------------------------------- |
| `PUT /v1/cache/{hash}` | Upload a task output   | `200` stored, `401` invalid token, `403` read-only token, `409` hash already exists |
| `GET /v1/cache/{hash}` | Download a task output | `200` artifact stream, `403` forbidden, `404` not found                             |

Both endpoints require an `Authorization: Bearer <token>` header.

## Development

```sh
make build     # compile to bin/nx-cache-server
make test      # go test -race -cover ./...
make lint      # golangci-lint run
make generate  # regenerate internal/api from openapi.json (ogen)
make docker    # multi-arch image build (linux/amd64, linux/arm64)
```
