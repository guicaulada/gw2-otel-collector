# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /out/gw2-collector ./cmd/gw2-collector

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gw2-collector /gw2-collector
# Exec form so SIGTERM reaches the process for graceful shutdown.
ENTRYPOINT ["/gw2-collector"]
