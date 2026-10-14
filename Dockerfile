# Build on the native platform and cross-compile for the target, which is
# much faster than emulating the Go compiler under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /nx-cache-server ./cmd/nx-cache-server

FROM alpine:3
# CA certificates are needed for TLS connections to S3.
RUN apk add --no-cache ca-certificates
COPY --from=build /nx-cache-server /nx-cache-server
EXPOSE 8080
ENTRYPOINT ["/nx-cache-server"]
