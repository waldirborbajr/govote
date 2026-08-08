# Secrets (GOVOTE_JWT_SECRET, GOVOTE_CPF_PEPPER) MUST come from runtime env/compose — never bake into the image.
# ====================== BUILD STAGE ======================
# --platform=$BUILDPLATFORM força este estágio a rodar sempre na
# arquitetura NATIVA do runner que está buildando (ex.: amd64 no GitHub
# Actions), mesmo que o alvo final seja arm64 (Raspberry Pi). O Go faz
# cross-compile de verdade (não precisa executar código arm64), então
# isso evita emulação via QEMU no CI — build bem mais rápido.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

# Preenchidos automaticamente pelo buildx a partir de --platform na hora
# do `docker buildx build` (ex.: TARGETOS=linux TARGETARCH=arm64).
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# 1. Copiar apenas os arquivos de dependências (melhor cache de camadas)
COPY go.mod go.sum ./
RUN go mod download

# 2. Instalar templ (para gerar código a partir dos .templ)
RUN go install github.com/a-h/templ/cmd/templ@v0.2.793

# 3. Copiar o código fonte (só invalida o cache quando o código muda)
COPY . .

# 4. Gerar código dos templates (Go + templ + HTMx)
RUN templ generate ./internal/views/

# 5. Build estático. CGO_ENABLED=0 é obrigatório e já é suficiente aqui:
#    modernc.org/sqlite é pure-Go (sem CGO) e golang.org/x/crypto/argon2
#    também, então não há nenhum link externo.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags="-s -w" \
    -o /govote ./cmd/govote

# ====================== PRODUCTION STAGE (menor possível) ======================
FROM scratch

# Diretório de dados: o binário abre "votes.db", "cert.pem" e "key.pem" com
# caminho relativo, então eles caem dentro do WORKDIR. Um único volume em
# /data cobre banco + certificado TLS autoassinado (persiste entre reinícios
# do container, evitando trocar o cert TLS a cada deploy).
WORKDIR /data

# Certificados CA
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Binário compilado (contém os templates gerados pelo templ embutidos)
COPY --from=builder /govote /govote

# Roda como não-root.
USER 65532:65532

EXPOSE 9080 8443

ENTRYPOINT ["/govote"]
