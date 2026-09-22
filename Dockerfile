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
# Leeres Datenverzeichnis, das dem non-root-User gehört (siehe unten).
RUN mkdir -p /out/data

# --- Runtime-Stage ---------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/wiki-rog /app/wiki-rog
# /data dem non-root-User (uid/gid 65532) übereignen, damit der Store dort
# schreiben kann. Ein leeres Named Volume erbt diese Rechte beim ersten Mounten.
COPY --from=build --chown=65532:65532 /out/data /data

# File-basierter Vektorspeicher unter /data (in k8s per PVC gemountet)
ENV STORE_DIR=/data/store \
    HTTP_ADDR=:8080

EXPOSE 8080
USER nonroot:nonroot

# Standard: Web-UI. Der Ingest-Job überschreibt das mit ["ingest"].
ENTRYPOINT ["/app/wiki-rog"]
CMD ["serve"]
