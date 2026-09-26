# syntax=docker/dockerfile:1

# --- build -----------------------------------------------------------------
# Pinned to the same minor as go.mod so the build fails loudly on a mismatch
# rather than producing a binary the module's language version does not allow.
FROM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies first: they change rarely, so this layer survives source edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static binary: CGO off means no libc at runtime, so the final image needs
# nothing but the binary itself. -trimpath keeps build paths out of the binary.
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.serverVersion=${VERSION}" \
    -o /out/jirrabit-mcp ./cmd/jirrabit-mcp/ \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/smoke ./cmd/smoke/ \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/healthcheck ./cmd/healthcheck/

# --- runtime ---------------------------------------------------------------
FROM alpine:3.22

# ca-certificates is for reaching HTTPS instances.
# tzdata is needed because jirrabit is timezone-aware and operators set
# JIRRABIT_*_TIMEZONE by name.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 mcp

COPY --from=build /out/jirrabit-mcp /usr/local/bin/jirrabit-mcp
COPY --from=build /out/smoke /usr/local/bin/smoke
COPY --from=build /out/healthcheck /usr/local/bin/healthcheck

USER mcp

# streamable HTTP by default: a containerised server is a service, and stdio
# only makes sense when the client runs the binary directly. Override with
# JIRRABIT_MCP_TRANSPORT=stdio when driving it through compose exec.
ENV JIRRABIT_MCP_TRANSPORT=http \
    JIRRABIT_MCP_ADDR=:8082 \
    JIRRABIT_MCP_PATH=/mcp

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD /usr/local/bin/healthcheck || exit 1

ENTRYPOINT ["/usr/local/bin/jirrabit-mcp"]
