# syntax=docker/dockerfile:1
# Base images pinned by digest (multi-arch index) so a rebuild of the same
# commit gets the same toolchain; bump tag and digest together.
FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
# Coolify passes the deployed commit as SOURCE_COMMIT; /healthz and the admin
# status page show it, so you can tell which build is live.
ARG SOURCE_COMMIT=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${SOURCE_COMMIT}" -o /out/scouter-aggregator ./cmd/scouter-aggregator
# /data must belong to the user the app runs as (distroless nonroot = 65532).
# Docker gives a new, empty named volume the ownership of this directory.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

# distroless/static: CA certificates for api.github.com, nothing else; runs as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/scouter-aggregator /scouter-aggregator
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 8080
USER nonroot
# The binary checks itself (exec form: there is no shell). Coolify's own
# health check must stay off: it runs curl through /bin/sh, which overrides this.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/scouter-aggregator", "healthcheck"]
ENTRYPOINT ["/scouter-aggregator"]
