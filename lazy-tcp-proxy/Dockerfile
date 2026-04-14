FROM --platform=$BUILDPLATFORM golang:1.26.2-alpine AS builder
ARG TARGETARCH
ARG TARGETVARIANT
ARG BUILD_TAGS=""
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} go build -tags "${BUILD_TAGS}" -a -trimpath -o lazy-tcp-proxy .

FROM scratch
COPY --from=builder /app/lazy-tcp-proxy /lazy-tcp-proxy
COPY ./LICENSE /LICENSE
ENV DOCKER_SOCK=/var/run/docker.sock
VOLUME ["/var/run/docker.sock"]
ENTRYPOINT ["/lazy-tcp-proxy"]
