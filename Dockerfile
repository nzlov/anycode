FROM archlinux:latest AS base
ARG PACMAN_MIRROR=
ARG GOPROXY=
ENV GOPROXY=$GOPROXY
RUN if [ -n "$PACMAN_MIRROR" ]; then \
  printf 'Server = %s\n' "$PACMAN_MIRROR" > /etc/pacman.d/mirrorlist; \
  fi

FROM base AS web
RUN pacman -Syu --noconfirm --needed nodejs npm \
  && pacman -Scc --noconfirm
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
COPY internal/interfaces/http/static/ /src/internal/interfaces/http/static/
RUN npm run build

FROM base AS build
RUN pacman -Syu --noconfirm --needed ca-certificates go git \
  && pacman -Scc --noconfirm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web /src/internal/interfaces/http/static/dist ./internal/interfaces/http/static/dist
RUN go build -o /out/anycode ./cmd/anycode

FROM base
ARG ANYCODE_UID=1000
ARG ANYCODE_GID=1000
ENV ANYCODE_UID=$ANYCODE_UID
ENV ANYCODE_GID=$ANYCODE_GID
ENV NVM_DIR=/usr/local/nvm
ENV NVM_SYMLINK_CURRENT=true
ENV PATH=/usr/local/nvm/current/bin:$PATH
RUN pacman -Syu --noconfirm --needed ca-certificates git bash nvm wget ripgrep p7zip openssh mdbook less go chromium ttf-dejavu \
  && pacman -Scc --noconfirm \
  && . /usr/share/nvm/init-nvm.sh \
  && nvm install node \
  && nvm alias default node \
  && npm install -g @openai/codex@latest \
  && npm cache clean --force \
  && groupadd --gid "$ANYCODE_GID" anycode \
  && useradd --uid "$ANYCODE_UID" --gid anycode --create-home --home-dir /home/anycode --shell /bin/bash anycode \
  && install -d -o anycode -g anycode /app /workspaces /home/anycode/.anycode /home/anycode/.codex \
  && git config --system --add safe.directory '/workspaces/*'
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
