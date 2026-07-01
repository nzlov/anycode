FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
COPY internal/interfaces/http/static/ /src/internal/interfaces/http/static/
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web /src/internal/interfaces/http/static/dist ./internal/interfaces/http/static/dist
RUN go build -o /out/anycode ./cmd/anycode

FROM alpine:3.22
RUN apk add --no-cache ca-certificates git
WORKDIR /app
COPY --from=build /out/anycode /usr/local/bin/anycode
EXPOSE 8080
ENTRYPOINT ["anycode"]
