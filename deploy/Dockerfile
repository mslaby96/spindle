FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26 AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/internal/webui/dist ./internal/webui/dist
COPY firmware/main.py firmware/ota.py ./internal/firmware/bundle/
COPY firmware/app ./internal/firmware/bundle/app
COPY firmware/lib ./internal/firmware/bundle/lib
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/spindle ./cmd/spindle
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/spindle /spindle
COPY --from=server --chown=nonroot:nonroot /out/data /data
ENV SPINDLE_DATA_DIR=/data
EXPOSE 8080
VOLUME /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/spindle", "healthcheck"]
ENTRYPOINT ["/spindle"]
CMD ["serve"]
