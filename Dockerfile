FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go run ./cmd/tagger -check
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/margit .

FROM alpine:3.22
RUN addgroup -S margit && adduser -S -G margit margit
WORKDIR /app
COPY --from=build /out/margit /app/margit
COPY docker/config.json /app/config.json
USER margit
EXPOSE 8080
ENV GOMAXPROCS=1 GOMEMLIMIT=900MiB MARGIT_LOG_LEVEL=info MARGIT_STORE_TYPE=memory \
    MARGIT_MAX_DEPTH=25 MARGIT_BLOOM_EXPECTED=1000000
HEALTHCHECK --interval=10s --timeout=3s --start-period=180s --start-interval=2s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/margit"]
CMD ["-config", "/app/config.json"]
