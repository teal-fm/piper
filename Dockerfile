FROM --platform=${BUILDPLATFORM:-linux/amd64} node:24-alpine3.21 as node_builder
WORKDIR /app
RUN npm install tailwindcss @tailwindcss/cli

COPY ./pages/templates /app/templates
COPY ./pages/static /app/static

RUN npx @tailwindcss/cli -i /app/static/base.css -o /app/static/main.css -m

FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.24.3-alpine3.21 as builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

#needed for sqlite
RUN apk add --update gcc musl-dev

# step 1. dep cache
WORKDIR /app
ARG TARGETPLATFORM=${BUILDPLATFORM:-linux/amd64}
COPY go.mod go.sum ./
RUN go mod download

# step 2. build the actual app
WORKDIR /app
COPY . .
#Overwrite the main.css with the one from the builder
COPY --from=node_builder /app/static/main.css /app/pages/static/main.css
 #generate the jwks
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags='-w -s -extldflags "-static"' -o main ./cmd
ARG TARGETOS=${TARGETPLATFORM%%/*}
ARG TARGETARCH=${TARGETPLATFORM##*/}

FROM --platform=${TARGETPLATFORM:-linux/amd64} alpine:3.21
#Creates an empty /db folder for docker compose
WORKDIR /db
WORKDIR /app
COPY --from=builder /app/main /app/main
COPY --from=builder /app/jwks.json /app/jwks.json
ENTRYPOINT ["/app/main"]
