# syntax=docker/dockerfile:1
#
# izci container image: the web UI and a static binary built in separate
# stages, running on Debian slim with git (for AI analyses) as a non-root
# user. Build through `make docker`, which passes the version details.
#
# The web UI uses the KentOS component kit from the sibling repository
# kentos-components, passed as a named build context:
#
#   docker build --build-context kentos-components=../kentos-components -t izci .
#   docker build --build-context kentos-components=../kentos-components \
#     --build-arg WITH_OPENCODE=true -t izci:ai .
#
# See docs/operasyon.md for running it.

ARG GO_VERSION=1.26
ARG NODE_VERSION=24
ARG DEBIAN_VERSION=trixie
# true adds the opencode CLI used by AI analyses (about 180 MB).
ARG WITH_OPENCODE=false

# --- Web UI ------------------------------------------------------------------
FROM node:${NODE_VERSION}-${DEBIAN_VERSION}-slim AS web
ARG PNPM_VERSION=11.17.0
RUN npm install --global --no-fund --no-audit "pnpm@${PNPM_VERSION}"
WORKDIR /src/web
# web/package.json refers to file:../../kentos-components/packages/components.
COPY --from=kentos-components packages/components /kentos-components/packages/components
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,id=izci-pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# --- Binary ------------------------------------------------------------------
FROM golang:${GO_VERSION}-${DEBIAN_VERSION} AS build
WORKDIR /src
COPY go.mod go.sum go.work go.work.sum ./
COPY sdk/go/go.mod sdk/go/go.sum sdk/go/
COPY sdk/go/izcipgx/go.mod sdk/go/izcipgx/go.sum sdk/go/izcipgx/
RUN --mount=type=cache,id=izci-gomod,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
ARG VERSION=dev
ARG COMMIT=
ARG DATE=
RUN --mount=type=cache,id=izci-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=izci-gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w \
      -X github.com/mcihad/kentos-izci/internal/version.Version=${VERSION} \
      -X github.com/mcihad/kentos-izci/internal/version.Commit=${COMMIT} \
      -X github.com/mcihad/kentos-izci/internal/version.Date=${DATE}" \
      -o /out/izci ./cmd/izci

# --- opencode (only built with WITH_OPENCODE=true) ---------------------------
FROM node:${NODE_VERSION}-${DEBIAN_VERSION}-slim AS opencode
ARG OPENCODE_VERSION=1.18.31
# npm verifies the package checksums; the platform package carries a
# standalone binary, so the runtime image needs no Node.js.
RUN npm install --global --ignore-scripts --no-fund --no-audit "opencode-ai@${OPENCODE_VERSION}" \
 && bin="$(find /usr/local/lib/node_modules -path '*/opencode-linux-*/bin/opencode' \
      ! -path '*-musl/*' ! -path '*-baseline*' | head -n 1)" \
 && test -n "$bin" \
 && install -m 0755 "$bin" /opencode

# --- Runtime -----------------------------------------------------------------
FROM debian:${DEBIAN_VERSION}-slim AS runtime-false
RUN apt-get update \
 && apt-get install --yes --no-install-recommends ca-certificates git \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --user-group --home-dir /var/lib/izci --create-home --shell /usr/sbin/nologin izci
COPY --from=build /out/izci /usr/local/bin/izci

FROM runtime-false AS runtime-true
# bubblewrap isolates the agent (AI_SANDBOX=bwrap).
RUN apt-get update \
 && apt-get install --yes --no-install-recommends bubblewrap \
 && rm -rf /var/lib/apt/lists/*
COPY --from=opencode /opencode /usr/local/bin/opencode

FROM runtime-${WITH_OPENCODE}
ARG VERSION=dev
ARG COMMIT=
LABEL org.opencontainers.image.title="izci" \
      org.opencontainers.image.description="Error tracking and aggregation for KentOS applications" \
      org.opencontainers.image.source="https://github.com/mcihad/kentos-izci" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"
ENV IZCI_ENV=production \
    IZCI_HTTP_ADDR=:5300 \
    IZCI_DATA_DIR=/var/lib/izci
USER izci
WORKDIR /var/lib/izci
VOLUME ["/var/lib/izci"]
EXPOSE 5300
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 CMD ["izci", "healthcheck"]
ENTRYPOINT ["izci"]
CMD ["serve"]
