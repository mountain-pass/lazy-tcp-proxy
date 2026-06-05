FROM --platform=$BUILDPLATFORM node:20-alpine AS html-builder
WORKDIR /app/html
COPY html/package.json html/package-lock.json ./
RUN npm ci
COPY html/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS builder
ARG TARGETARCH
ARG TARGETVARIANT
ARG BUILD_TAGS=""
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=html-builder /app/html/dist ./html/dist
RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} go build -tags "${BUILD_TAGS}" -a -trimpath -o lazy-tcp-proxy .

FROM scratch
COPY --from=builder /app/lazy-tcp-proxy /lazy-tcp-proxy
COPY ./LICENSE /LICENSE
ENV DOCKER_SOCK=/var/run/docker.sock
VOLUME ["/var/run/docker.sock"]
ENTRYPOINT ["/lazy-tcp-proxy"]
