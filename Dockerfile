# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS node_builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY ./pages ./pages
RUN npm run build:css

# Cross-compile on the builder architecture. Running the Go compiler under QEMU
# makes ARM64 builds more than ten minutes slower on GitHub-hosted runners.
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.8.0@sha256:add602d55daca18914838a78221f6bbe4284114b452c86a48f96d59aeb00f5c6 AS xx

FROM --platform=$BUILDPLATFORM golang:1.24.3-alpine3.21 AS builder
COPY --from=xx / /
RUN apk add --no-cache clang lld
ARG TARGETPLATFORM
RUN xx-apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=node_builder /app/pages/static/main.css ./pages/static/main.css
ARG PIPER_BUILD_CHANNEL=main
ARG PIPER_BUILD_REVISION
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 xx-go build -ldflags="-w -s -extldflags '-static' -X github.com/teal-fm/piper/models.buildChannel=${PIPER_BUILD_CHANNEL} -X github.com/teal-fm/piper/models.buildRevision=${PIPER_BUILD_REVISION}" -o main ./cmd \
    && xx-verify --static main

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S piper \
    && adduser -S -G piper piper \
    && mkdir -p /db \
    && chown piper:piper /db
WORKDIR /db
WORKDIR /app
COPY --from=builder --chown=piper:piper /app/main /app/main
USER piper
ENTRYPOINT ["/app/main"]
