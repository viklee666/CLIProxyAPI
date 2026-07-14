FROM node:22-alpine AS manager-web

ARG VERSION=dev
WORKDIR /manager
COPY extensions/cpa-manager-plus/package*.json ./
COPY extensions/cpa-manager-plus/apps/web/package.json ./apps/web/package.json
RUN npm ci
COPY extensions/cpa-manager-plus/apps/web ./apps/web
RUN CPA_SINGLE_FILE=false VERSION=${VERSION} npm --workspace apps/web run build

FROM golang:1.26-bookworm AS builder

ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends build-essential git && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./

RUN go mod download

COPY extensions/cpa-manager-plus/apps/manager-server/go.mod extensions/cpa-manager-plus/apps/manager-server/go.sum /tmp/manager-server/
RUN cd /tmp/manager-server && go mod download

COPY . .

COPY --from=manager-web /manager/apps/web/dist /app/extensions/cpa-manager-plus/apps/manager-server/internal/httpapi/web
RUN mv /app/extensions/cpa-manager-plus/apps/manager-server/internal/httpapi/web/index.html /app/extensions/cpa-manager-plus/apps/manager-server/internal/httpapi/web/management.html

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=1 GOOS=linux go build -buildvcs=false -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" -o ./CLIProxyAPI ./cmd/server/

RUN cd /app/extensions/cpa-manager-plus/apps/manager-server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w" -o /app/cpa-manager-plus ./cmd/cpa-manager-plus

FROM debian:bookworm

RUN apt-get update && apt-get install -y --no-install-recommends tzdata ca-certificates wget tini && rm -rf /var/lib/apt/lists/*

RUN mkdir /CLIProxyAPI

COPY --from=builder ./app/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI
COPY --from=builder ./app/cpa-manager-plus /CLIProxyAPI/cpa-manager-plus

COPY config.example.yaml /CLIProxyAPI/config.example.yaml
COPY --from=manager-web /manager/apps/web/dist/index.html /CLIProxyAPI/static/management.html
COPY deploy/cli/entrypoint.sh /CLIProxyAPI/entrypoint.sh

WORKDIR /CLIProxyAPI

EXPOSE 8317

ENV TZ=Asia/Shanghai
ENV MANAGEMENT_STATIC_PATH=/CLIProxyAPI/static/management.html
ENV MANAGEMENT_STATIC_IMMUTABLE=true
ENV CPA_MANAGER_PLUS_URL=http://127.0.0.1:18317
ENV CPA_MANAGER_INTEGRATED=true
ENV HTTP_ADDR=127.0.0.1:18317
ENV CPA_UPSTREAM_URL=http://127.0.0.1:8317
ENV USAGE_DATA_DIR=/data
ENV USAGE_DB_PATH=/data/usage.sqlite
ENV CLIENT_ACCESS_ENABLED=true
ENV CLIENT_ACCESS_DB_PATH=/data/client-access.sqlite

RUN chmod +x /CLIProxyAPI/entrypoint.sh && \
    cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

ENTRYPOINT ["/usr/bin/tini", "-g", "--", "/CLIProxyAPI/entrypoint.sh"]
