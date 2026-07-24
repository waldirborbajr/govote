########################################
# Stage 1 - Go toolchain
########################################
FROM golang:1.26 AS golang-tools

ENV CGO_ENABLED=1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

ENV GOBIN=/opt/go-tools/bin

RUN mkdir -p ${GOBIN}

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

########################################
# Stage 2 - DevContainer
########################################
FROM mcr.microsoft.com/devcontainers/go:1-1.26-bookworm

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
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
        lsof \
        unzip && \
    rm -rf /var/lib/apt/lists/*

COPY --from=golang-tools /opt/go-tools/bin/ /usr/local/bin/

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org

RUN cat >/etc/profile.d/go-tools.sh <<'EOF'
export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"

alias ll="ls -lah"
alias fmt="gofumpt -w ."
alias lint="golangci-lint run"
alias test="gotestsum"
alias air="air"
EOF

USER vscode

WORKDIR /workspaces

RUN go version && \
    gopls version && \
    golangci-lint version && \
    staticcheck -debug.version
