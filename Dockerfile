# syntax=docker/dockerfile:1

# --- build -----------------------------------------------------------------
# Pinned to the same minor as go.mod so the build fails loudly on a mismatch
# rather than producing a binary the module's language version does not allow.
FROM golang:1.27-alpine AS builder

# TARGETOS and TARGETARCH are declared WITHOUT a default on purpose.
#
# BuildKit injects both from the requested platform, but a declared default wins
# over the injected value. `ARG TARGETARCH=amd64` therefore compiles amd64 code
# even under `buildx build --platform linux/arm64`, and the result is an image
# whose config says arm64 and whose binary is x86-64: it pulls fine and then
# fails with "exec format error" on every ARM host, which is exactly the machine
# most likely to be running a self-hosted jirrabit. Declaring them bare makes
# both paths correct — buildx fills in the requested platform, and a plain
# `docker build` fills in the host's.
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

# ca-certificates is only needed here so the final stage can copy the bundle
# out; the builder itself makes no outbound requests.
RUN apk add --no-cache ca-certificates

WORKDIR /src

# Dependencies first: they change rarely, so this layer survives source edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static, because the runtime stage is scratch and has no libc to link against.
# -X stamps the version the server reports over MCP in its initialize response,
# which is how an operator tells which build they are running; it only reaches
# a variable, so main.serverVersion has to be one.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.serverVersion=${VERSION}" \
      -o /out/jirrabit-mcp ./cmd/jirrabit-mcp/ \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w" -o /out/healthcheck ./cmd/healthcheck/

# --- runtime ---------------------------------------------------------------
# scratch, not alpine: the server opens outbound TLS to somebody else's
# jirrabit and listens on one port, and never needs a shell, a package manager
# or a filesystem. That is ~8 MB of libc and apk that would never be executed.
#
# The one thing a scratch image does not have is /etc/ssl/certs, and the one it
# cannot do is resolve a name — both matter here, because the caller's
# instanceUrl is a hostname this server has to look up. Docker bind-mounts
# /etc/resolv.conf into every container including scratch ones, so DNS comes
# from the runtime; the certificate bundle has to be copied in by hand.
FROM scratch

COPY --from=builder /out/jirrabit-mcp /jirrabit-mcp
COPY --from=builder /out/healthcheck /healthcheck
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# streamable HTTP by default: a containerised server is a service, and stdio
# only makes sense when the client runs the binary directly. Override with
# JIRRABIT_MCP_TRANSPORT=stdio when driving it through compose exec.
ENV JIRRABIT_MCP_TRANSPORT=http \
    JIRRABIT_MCP_ADDR=:8082 \
    JIRRABIT_MCP_PATH=/mcp

EXPOSE 8082

# Exec form, not shell form: scratch has no /bin/sh to run a shell-form
# HEALTHCHECK, and the failure mode is a container that reports healthy because
# the check could not run. The probe reads JIRRABIT_MCP_ADDR and
# JIRRABIT_MCP_PATH, so it follows the port the server was actually given, and
# it sends a real MCP initialize rather than checking that a port is open.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/healthcheck"]

# 65534 is nobody. scratch has no /etc/passwd, so a name would not resolve and
# the only way to drop root is a numeric id.
USER 65534:65534

ENTRYPOINT ["/jirrabit-mcp"]

LABEL org.opencontainers.image.source="https://github.com/rroblf01/jirrabit-mcp"
LABEL org.opencontainers.image.description="MCP server for jirrabit, using Atlassian's Jira tool vocabulary"
LABEL org.opencontainers.image.licenses="MIT"
