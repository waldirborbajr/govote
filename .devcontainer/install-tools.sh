#!/usr/bin/env bash

set -euo pipefail

echo "==> Updating packages..."

sudo apt-get update

sudo apt-get install -y \
    zsh \
    curl \
    wget \
    git \
    make \
    gcc \
    g++ \
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
    unzip

if [ ! -d "$HOME/.oh-my-zsh" ]; then
    echo "==> Installing Oh My Zsh..."
    RUNZSH=no CHSH=no KEEP_ZSHRC=yes \
        sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
fi

mkdir -p "$HOME/go/bin"

echo "==> Installing Go tools..."

go install golang.org/x/tools/gopls@latest

go install mvdan.cc/gofumpt@latest

go install golang.org/x/tools/cmd/goimports@latest

go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

go install honnef.co/go/tools/cmd/staticcheck@latest

go install golang.org/x/vuln/cmd/govulncheck@latest

go install github.com/go-delve/delve/cmd/dlv@latest

go install github.com/air-verse/air@latest

go install gotest.tools/gotestsum@latest

go install github.com/kyoh86/richgo@latest

if ! grep -q 'HOME/go/bin' ~/.zshrc; then
cat <<'EOF' >> ~/.zshrc

export PATH="$HOME/go/bin:$PATH"

alias ll="ls -lah"
alias gst="git status"
alias got="gotestsum --format testname"
alias lint="golangci-lint run"
alias fmt="gofumpt -w ."
alias imports="goimports -w ."

EOF
fi

echo
echo "========================================"
echo "Go Mega Power Environment Ready 🚀"
echo "========================================"

go version