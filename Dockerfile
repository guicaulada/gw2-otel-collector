# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.26@sha256:32c0e6e5c4f6707717051091b4d0b077464a679eaab563e11474efc5328e2aa5 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /out/gw2-collector ./cmd/gw2-collector
# Empty state dir owned by the nonroot uid; a named volume mounted at /data
# inherits this ownership so the nonroot process can write its bbolt file.
RUN mkdir -p /state

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot@sha256:d093aa3e30dbadd3efe1310db061a14da60299baff8450a17fe0ccc514a16639
COPY --from=build /out/gw2-collector /gw2-collector
COPY --from=build --chown=65532:65532 /state /data
# Exec form so SIGTERM reaches the process for graceful shutdown.
ENTRYPOINT ["/gw2-collector"]
