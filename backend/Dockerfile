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
ENV HTTP_ADDR=:8080 \
    DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/hippo", "healthcheck"]
ENTRYPOINT ["/hippo"]
