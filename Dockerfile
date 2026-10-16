# Imagen de hermes-mcp con transporte HTTP (por ejemplo, para Open WebUI).
# La configuracion y el fichero de conocimiento NO van en la imagen: se pasan
# al arrancar, para que cambiarlos no exija reconstruirla
# (ver docs/03-instalacion-y-configuracion.md).
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hermes-mcp ./cmd/hermes-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /hermes-mcp /hermes-mcp
ENV HERMES_MCP_TRANSPORTE=http \
    HERMES_MCP_HTTP_DIRECCION=:8080 \
    HERMES_MCP_SALIDAS_DIR=/salidas
EXPOSE 8080
ENTRYPOINT ["/hermes-mcp"]
