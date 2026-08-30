# Multi-stage image for the admin service (RF-16).
#
# Built from the repository root, not from this directory: the go.work of the root is what
# resolves the pkg module (01 section 3, no per-service replace), so the build context has
# to include it.
#
#   docker build -f services/admin/Dockerfile --target production .

# --- builder ----------------------------------------------------------------
FROM golang:1.27-alpine AS builder

# git is needed by the module downloader; the rest of the toolchain is not.
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# The manifests are copied first so the dependency layer is cached and only invalidated
# when a go.mod or go.sum actually changes.
COPY go.work go.work.sum* ./
COPY go.mod ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY services/identity/go.mod ./services/identity/
COPY services/tours/go.mod ./services/tours/
COPY services/booking/go.mod ./services/booking/
COPY services/admin/go.mod ./services/admin/
COPY services/mail-dispatcher/go.mod ./services/mail-dispatcher/

RUN go mod download

COPY pkg/ ./pkg/
COPY services/admin/ ./services/admin/

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# CGO off and a stripped binary, so the production stage can be a scratch-like image with
# no libc at all.
ENV CGO_ENABLED=0 GOOS=linux

RUN go build \
        -trimpath \
        -ldflags="-s -w \
            -X github.com/nicodanke/dizen-v2-backend/pkg/version.Version=${VERSION} \
            -X github.com/nicodanke/dizen-v2-backend/pkg/version.Commit=${COMMIT} \
            -X github.com/nicodanke/dizen-v2-backend/pkg/version.BuildDate=${BUILD_DATE}" \
        -o /out/server \
        ./services/admin/cmd/server

# --- development ------------------------------------------------------------
# Hot reload with Air. The source is bind-mounted by compose, so this stage carries the
# toolchain but none of the code.
FROM golang:1.27-alpine AS development

RUN apk add --no-cache git ca-certificates curl \
    && go install github.com/air-verse/air@v1.67.4

WORKDIR /src/services/admin

ENV CGO_ENABLED=0

EXPOSE 8080 9090

# curl rather than the binary itself: during a rebuild the binary may not exist for a
# moment, and the container must not be declared unhealthy for that.
HEALTHCHECK --interval=10s --timeout=3s --start-period=40s --retries=5 \
    CMD curl -fsS http://localhost:8080/livez || exit 1

CMD ["air", "-c", ".air.toml"]

# --- production -------------------------------------------------------------
# distroless static: no shell, no package manager, nothing to pivot to.
FROM gcr.io/distroless/static-debian12:nonroot AS production

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/server /server

# nonroot (uid 65532) comes from the base image; it is stated explicitly so a base change
# cannot silently promote the process to root.
USER 65532:65532

EXPOSE 8080 9090

# The binary probes itself: distroless has no curl and no shell. --healthcheck exits 0 when
# /livez answers.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/server", "--healthcheck"]

ENTRYPOINT ["/server"]
