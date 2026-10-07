# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scouter-aggregator ./cmd/scouter-aggregator

# distroless/static: CA certificates for api.github.com, nothing else; runs as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/scouter-aggregator /scouter-aggregator
VOLUME /data
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/scouter-aggregator"]
