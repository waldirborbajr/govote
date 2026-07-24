#!/bin/bash

set -e


echo "🔧 Installing system packages..."


sudo apt-get update

sudo apt-get install -y \
    zsh \
    ripgrep \
    fd-find \
    bat \
    git-delta \
    hyperfine \
    make \
    gcc \
    g++ \
    curl \
    jq \
    tree \
    htop \
    lsof \
    sqlite3 \
    libsqlite3-dev


echo "🐚 Installing Oh My Zsh..."

if [ ! -d "$HOME/.oh-my-zsh" ]; then
    sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended
fi


echo "📦 Installing Go development tools..."


go install github.com/air-verse/air@latest

go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

go install mvdan.cc/gofumpt@latest

go install golang.org/x/tools/cmd/goimports@latest

go install honnef.co/go/tools/cmd/staticcheck@latest

go install golang.org/x/vuln/cmd/govulncheck@latest

go install github.com/go-delve/delve/cmd/dlv@latest

go install gotest.tools/gotestsum@latest

go install github.com/kyoh86/richgo@latest


echo "🧪 Installing useful Go helpers..."

go install github.com/jesseduffield/lazygit@latest


echo "⚡ Configuring shell..."


cat >> ~/.zshrc <<'EOF'

export PATH=$PATH:$HOME/go/bin

alias ll="ls -lah"
alias la="ls -la"

alias gst="git status"
alias gco="git checkout"

alias got="gotestsum --format testname"

EOF


echo ""
echo "====================================="
echo "🚀 Go Mega Power Environment Loaded"
echo "====================================="

echo ""
go version
