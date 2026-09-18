# syntax=docker/dockerfile:1

# --- Build-Stage -----------------------------------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src

# Abhängigkeiten (nur Standardbibliothek -> go.mod genügt)
COPY go.mod ./
RUN go mod download

COPY . .
# Statisches Binary (CGO aus), damit es in einem minimalen Image läuft.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/wiki-rog ./cmd/wiki-rog

# --- Runtime-Stage ---------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/wiki-rog /app/wiki-rog

# ChromaDB-Ersatz: file-basierter Store unter /data (in k8s per PVC gemountet)
ENV STORE_DIR=/data/store \
    HTTP_ADDR=:8080

EXPOSE 8080
USER nonroot:nonroot

# Standard: Web-UI. Der Ingest-Job überschreibt das mit ["ingest"].
ENTRYPOINT ["/app/wiki-rog"]
CMD ["serve"]
