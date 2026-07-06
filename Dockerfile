FROM archlinux:base AS web
RUN pacman -Syu --noconfirm --needed nodejs npm \
  && pacman -Scc --noconfirm
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
COPY internal/interfaces/http/static/ /src/internal/interfaces/http/static/
RUN npm run build

FROM archlinux:base AS build
RUN pacman -Syu --noconfirm --needed ca-certificates go git \
  && pacman -Scc --noconfirm
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web /src/internal/interfaces/http/static/dist ./internal/interfaces/http/static/dist
RUN go build -o /out/anycode ./cmd/anycode

FROM archlinux:base
ARG CODEX_NPM_PACKAGE=@openai/codex
ARG ANYCODE_UID=1000
ARG ANYCODE_GID=1000
ENV ANYCODE_UID=$ANYCODE_UID
ENV ANYCODE_GID=$ANYCODE_GID
RUN pacman -Syu --noconfirm --needed ca-certificates git nodejs npm wget \
  && pacman -Scc --noconfirm \
  && groupadd --gid "$ANYCODE_GID" anycode \
  && useradd --uid "$ANYCODE_UID" --gid anycode --create-home --home-dir /home/anycode --shell /bin/bash anycode \
  && install -d -o anycode -g anycode /app /data /workspaces /home/anycode/.codex \
  && git config --system --add safe.directory '/workspaces/*' \
  && npm install -g "$CODEX_NPM_PACKAGE" \
  && npm cache clean --force
WORKDIR /app
COPY --from=build /out/anycode /usr/local/bin/anycode
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chown anycode:anycode /usr/local/bin/anycode
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
ENV HOME=/home/anycode
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["anycode"]
