# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scouter-aggregator ./cmd/scouter-aggregator
# /data must belong to the user the app runs as (distroless nonroot = 65532).
# Docker gives a new, empty named volume the ownership of this directory.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

# distroless/static: CA certificates for api.github.com, nothing else; runs as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/scouter-aggregator /scouter-aggregator
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/scouter-aggregator"]
