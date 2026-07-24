#!/usr/bin/env bash

set -euo pipefail

sudo apt-get update

sudo apt-get install -y \
    ripgrep \
    fd-find \
    bat \
    git-delta \
    hyperfine \
    jq \
    tree \
    htop \
    lsof

if [ ! -d ~/.oh-my-zsh ]; then
    RUNZSH=no CHSH=no KEEP_ZSHRC=yes \
    sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
fi

TOOLS=(
    "golang.org/x/tools/gopls@latest"
    "golang.org/x/tools/cmd/goimports@latest"
    "mvdan.cc/gofumpt@latest"
    "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
    "honnef.co/go/tools/cmd/staticcheck@latest"
    "golang.org/x/vuln/cmd/govulncheck@latest"
    "github.com/go-delve/delve/cmd/dlv@latest"
    "github.com/air-verse/air@latest"
    "gotest.tools/gotestsum@latest"
    "github.com/kyoh86/richgo@latest"
)

for tool in "${TOOLS[@]}"; do
    go install "$tool"
done

if ! grep -q 'go/bin' ~/.zshrc; then
cat <<EOF >> ~/.zshrc

export PATH="\$HOME/go/bin:\$PATH"

alias ll="ls -lah"
alias lint="golangci-lint run"
alias fmt="gofumpt -w ."
alias test="gotestsum"
alias air="air"

EOF
fi

go version

golangci-lint --version

echo
echo "✅ Go Mega Power ready"