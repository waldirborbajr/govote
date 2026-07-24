FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    curl \
    wget \
    git \
    ca-certificates \
    build-essential \
    zsh \
    unzip \
    sudo \
    sqlite3 \
    libsqlite3-dev \
    make \
    gcc \
    g++ \
    && rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.26.0

RUN wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    rm -rf /usr/local/go && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

ENV PATH="/usr/local/go/bin:/home/vscode/go/bin:${PATH}"

RUN useradd -ms /bin/zsh vscode && \
    echo "vscode ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/vscode

USER vscode

WORKDIR /workspace