# Hippo backend image (ADR-0019): static binary on distroless, non-root, about 20 MB.
# Build from the repository root: docker build -f backend/Dockerfile backend
# (the CI and `make image` do it), or from backend/: docker build .

FROM --platform=$BUILDPLATFORM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/hippo ./cmd/hippo

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hippo /hippo
# BACKUP_DIR has no VOLUME on purpose (nightly-backup, design D15): a missing mount must leave no
# /backups, so that the service refuses to start instead of backing up into an anonymous volume.
ENV HTTP_ADDR=:8080 \
    DATA_DIR=/data \
    BACKUP_DIR=/backups
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/hippo", "healthcheck"]
ENTRYPOINT ["/hippo"]
