ARG NODE_VERSION=24
ARG GO_VERSION=1.25
ARG DEBIAN_CODENAME=bookworm

FROM node:${NODE_VERSION}-${DEBIAN_CODENAME} AS frontend

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web ./
RUN npm run build


FROM golang:${GO_VERSION}-${DEBIAN_CODENAME} AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY --from=frontend /src/web/dist ./cmd/server/ui

RUN go test -mod=readonly ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -mod=readonly \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/go-bdinfo \
    ./cmd/server


FROM scratch

ARG APP_VERSION=dev
ARG VCS_REF=unknown
ARG UPSTREAM_VERSION=unknown
ARG UPSTREAM_COMMIT=unknown

LABEL org.opencontainers.image.title="BDInfo" \
      org.opencontainers.image.description="Browser-based Blu-ray structure and bitrate analysis powered by autobrr/go-bdinfo" \
      org.opencontainers.image.source="https://github.com/pegasbur/bdinfo" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      io.pegasbur.bdinfo.upstream.version="${UPSTREAM_VERSION}" \
      io.pegasbur.bdinfo.upstream.revision="${UPSTREAM_COMMIT}"

COPY --from=build /out/go-bdinfo /go-bdinfo

EXPOSE 4646

ENTRYPOINT ["/go-bdinfo"]
