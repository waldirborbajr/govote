###############################################################################
# Stage 1 - Build Go development tools
###############################################################################

ARG GO_VERSION=1.26.0

FROM golang:${GO_VERSION}-bookworm AS tools

ARG GOLANGCI_LINT_VERSION=v2.5.0
ARG GOFUMPT_VERSION=v0.9.2
ARG GOIMPORTS_VERSION=v0.38.0
ARG GOPLS_VERSION=v0.21.0
ARG STATICCHECK_VERSION=2025.1.1
ARG GOVULNCHECK_VERSION=v1.1.4
ARG DLV_VERSION=v1.25.2
ARG AIR_VERSION=v1.63.0
ARG GOTESTSUM_VERSION=v1.13.0
ARG RICHGO_VERSION=v0.3.13

ENV GOBIN=/opt/go-tools/bin

RUN mkdir -p ${GOBIN}

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install golang.org/x/tools/gopls@${GOPLS_VERSION} && \
    go install golang.org/x/tools/cmd/goimports@${GOIMPORTS_VERSION} && \
    go install mvdan.cc/gofumpt@${GOFUMPT_VERSION} && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} && \
    go install honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION} && \
    go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} && \
    go install github.com/go-delve/delve/cmd/dlv@${DLV_VERSION} && \
    go install github.com/air-verse/air@${AIR_VERSION} && \
    go install gotest.tools/gotestsum@${GOTESTSUM_VERSION} && \
    go install github.com/kyoh86/richgo@${RICHGO_VERSION}

###############################################################################
# Stage 2 - DevContainer
###############################################################################

FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

ARG GO_VERSION=1.26.0

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        gcc \
        g++ \
        make \
        curl \
        wget \
        unzip \
        ca-certificates \
        gnupg \
        lsb-release \
        sqlite3 \
        libsqlite3-dev \
        ripgrep \
        fd-find \
        bat \
        git \
        git-delta \
        hyperfine \
        jq \
        tree \
        htop \
        lsof && \
    rm -rf /var/lib/apt/lists/*

###############################################################################
# GitHub CLI
###############################################################################

RUN mkdir -p -m 755 /etc/apt/keyrings && \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | tee /etc/apt/keyrings/githubcli-archive-keyring.gpg >/dev/null && \
    chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    apt-get install -y gh && \
    rm -rf /var/lib/apt/lists/*

###############################################################################
# just
###############################################################################

RUN wget -qO- https://github.com/casey/just/releases/download/1.43.0/just-1.43.0-x86_64-unknown-linux-musl.tar.gz \
    | tar -xz -C /usr/local/bin just

###############################################################################
# Go SDK
###############################################################################

RUN wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    rm -rf /usr/local/go && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

###############################################################################
# Go tools
###############################################################################

COPY --from=tools /opt/go-tools/bin/ /usr/local/bin/

###############################################################################
# Environment
###############################################################################

ENV PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org

###############################################################################
# Shell
###############################################################################

RUN cat >> /etc/bash.bashrc <<'EOF'

alias ll='ls -lah'
alias fmt='gofumpt -w .'
alias lint='golangci-lint run'
alias test='gotestsum'
alias air='air'

EOF

USER vscode

WORKDIR /workspaces

###############################################################################
# Smoke Test
###############################################################################

RUN go version && \
    gopls version && \
    goimports -h >/dev/null && \
    gofumpt -version && \
    golangci-lint version && \
    staticcheck -debug.version && \
    govulncheck -version && \
    dlv version && \
    air -v && \
    gotestsum --version && \
    richgo -h >/dev/null && \
    gh --version && \
    just --version

###############################################################################
# Healthcheck
###############################################################################

HEALTHCHECK --interval=5m --timeout=10s --start-period=30s --retries=3 \
CMD go version >/dev/null 2>&1 && \
    gopls version >/dev/null 2>&1 && \
    golangci-lint version >/dev/null 2>&1 || exit 1
