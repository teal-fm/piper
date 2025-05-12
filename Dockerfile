FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:latest as builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

# step 1. dep cache
WORKDIR /app
ARG TARGETPLATFORM=${BUILDPLATFORM:-linux/amd64}
COPY go.mod go.sum ./
RUN go mod download

# step 2. build the actual app
WORKDIR /app
COPY . .
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o main ./cmd
ARG TARGETOS=${TARGETPLATFORM%%/*}
ARG TARGETARCH=${TARGETPLATFORM##*/}

FROM --platform=${TARGETPLATFORM:-linux/amd64} scratch
WORKDIR /app/
COPY --from=builder /app/main /app/main
ENTRYPOINT ["/app/main"]
