FROM golang:1.26 AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
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
