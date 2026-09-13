FROM --platform=$BUILDPLATFORM node:24-alpine AS node_builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY ./pages ./pages
RUN npm run build:css

# Build on the target architecture so SQLite's CGO code uses the correct compiler.
FROM golang:1.24.3-alpine3.21 AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=node_builder /app/pages/static/main.css ./pages/static/main.css
RUN CGO_ENABLED=1 go build -ldflags='-w -s -extldflags "-static"' -o main ./cmd

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
