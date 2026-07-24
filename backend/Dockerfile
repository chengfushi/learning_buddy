# backend 多阶段构建
FROM golang:1.25 AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    for attempt in 1 2 3; do \
      go mod download && exit 0; \
      echo "go mod download failed (attempt ${attempt}/3), retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /out/backend .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/backend /app/backend
COPY --from=build /out/healthcheck /app/healthcheck
EXPOSE 8080
ENTRYPOINT ["/app/backend"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 CMD ["/app/healthcheck"]
