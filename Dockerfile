FROM node:26.11.1-alpine3.23 AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.2-alpine3.23 AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seliworks ./cmd/seliworks

FROM alpine:3.24.2 AS runtime
RUN apk add --no-cache ca-certificates=20260909-r0 tzdata=2026e-r0 wget=1.25.0-r3 && \
    addgroup -g 10001 seliworks && adduser -D -H -u 10001 -G seliworks seliworks && \
    mkdir -p /app/web/dist /var/lib/seliworks && chown 10001:10001 /var/lib/seliworks
WORKDIR /app
COPY --from=backend /out/seliworks /usr/local/bin/seliworks
COPY --from=frontend /src/web/dist/ /app/web/dist/
ENV LISTEN_ADDR=:8080 STATIC_DIR=/app/web/dist DATA_DIR=/var/lib/seliworks
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/seliworks"]
CMD ["serve"]
