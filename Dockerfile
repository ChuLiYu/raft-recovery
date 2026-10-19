# syntax=docker/dockerfile:1

FROM golang:1.25.1-bookworm AS deps-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM deps-base AS build-base
COPY . .

FROM build-base AS runtime-build
ARG TARGETOS=linux
ARG TARGETARCH
ENV CGO_ENABLED=1
RUN GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -ldflags='-s -w' -o /out/queue ./cmd/queue \
    && GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -ldflags='-s -w' -o /out/e2e-probe ./test/e2e

FROM debian:bookworm-slim AS runtime
RUN useradd --system --uid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin raft \
    && mkdir -p /data \
    && chown 10001:10001 /data
COPY --from=runtime-build /out/queue /usr/local/bin/queue
COPY --from=runtime-build /out/e2e-probe /usr/local/bin/e2e-probe
COPY configs/container.yaml /etc/raft/container.yaml
USER 10001:10001
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/queue"]

FROM deps-base AS tools
ARG PROTOC_VERSION=33.4
ARG TARGETARCH
RUN case "$TARGETARCH" in \
      arm64) protoc_arch=aarch_64 ;; \
      amd64) protoc_arch=x86_64 ;; \
      *) echo "unsupported target architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl unzip \
    && curl -fsSL "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${protoc_arch}.zip" -o /tmp/protoc.zip \
    && unzip -q /tmp/protoc.zip -d /usr/local \
    && rm -rf /tmp/protoc.zip /var/lib/apt/lists/* \
    && chmod +x /usr/local/bin/protoc
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.8 \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
COPY . .
RUN groupadd --gid 10001 raft \
    && useradd --uid 10001 --gid 10001 --home-dir /home/raft --create-home \
      --shell /usr/sbin/nologin raft \
    && mkdir -p /home/raft/.cache/go-build \
    && chown -R 10001:10001 /home/raft /src
ENV HOME=/home/raft \
    GOPATH=/go \
    GOMODCACHE=/go/pkg/mod \
    GOCACHE=/home/raft/.cache/go-build \
    PATH="/go/bin:${PATH}"
USER 10001:10001
