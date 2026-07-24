###############################################
# Stage 1 - Go SDK + Development Tools
###############################################
ARG GO_VERSION=1.26.0

FROM golang:${GO_VERSION}-bookworm AS go-builder

ENV CGO_ENABLED=1 \
    GOBIN=/opt/go/bin

RUN mkdir -p ${GOBIN}

# Instala todas as ferramentas Go
RUN go install golang.org/x/tools/gopls@latest && \
    go install golang.org/x/tools/cmd/goimports@latest && \
    go install mvdan.cc/gofumpt@latest && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install honnef.co/go/tools/cmd/staticcheck@latest && \
    go install golang.org/x/vuln/cmd/govulncheck@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest && \
    go install github.com/air-verse/air@latest && \
    go install gotest.tools/gotestsum@latest && \
    go install github.com/kyoh86/richgo@latest

###############################################
# Stage 2 - DevContainer
###############################################
FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        gcc \
        g++ \
        make \
        git \
        curl \
        wget \
        unzip \
        ca-certificates \
        sqlite3 \
        libsqlite3-dev \
        ripgrep \
        fd-find \
        bat \
        git-delta \
        hyperfine \
        jq \
        tree \
        htop \
        lsof && \
    rm -rf /var/lib/apt/lists/*

#
# Copia o Go SDK completo
#
COPY --from=go-builder /usr/local/go /usr/local/go

#
# Copia todas as ferramentas Go
#
COPY --from=go-builder /opt/go/bin/ /usr/local/bin/

ENV PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org

#
# Configuração global do shell
#
RUN printf '\n\
alias ll="ls -lah"\n\
alias fmt="gofumpt -w ."\n\
alias lint="golangci-lint run"\n\
alias test="gotestsum"\n\
alias air="air"\n\
' >> /etc/bash.bashrc

USER vscode

WORKDIR /workspaces

#
# Smoke tests (falha o build se alguma ferramenta não existir)
#
RUN go version && \
    gopls version && \
    gofumpt -version && \
    golangci-lint version && \
    staticcheck -debug.version && \
    dlv version && \
    govulncheck -version
