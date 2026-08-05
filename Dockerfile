# Secrets (GOVOTE_JWT_SECRET, GOVOTE_CPF_PEPPER) MUST come from runtime env/compose — never bake into the image.
# ====================== BUILD STAGE ======================
# go.mod exige "go 1.26" — usar imagem mais antiga faz o build falhar
# com "go.mod requires go >= 1.26".
#
# --platform=$BUILDPLATFORM força este estágio a rodar sempre na
# arquitetura NATIVA do runner que está buildando (ex.: amd64 no GitHub
# Actions), mesmo que o alvo final seja arm64 (Raspberry Pi). O Go faz
# cross-compile de verdade (não precisa executar código arm64), então
# isso evita emulação via QEMU no CI — build bem mais rápido.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

# Preenchidos automaticamente pelo buildx a partir de --platform na hora
# do `docker buildx build` (ex.: TARGETOS=linux TARGETARCH=arm64).
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# 1. Copiar apenas os arquivos de dependências (melhor cache de camadas)
COPY go.mod go.sum ./
RUN go mod download

# 2. Copiar o código fonte (só invalida o cache quando o código muda)
COPY . .

# 3. Build estático. CGO_ENABLED=0 é obrigatório e já é suficiente aqui:
#    modernc.org/sqlite é pure-Go (sem CGO) e golang.org/x/crypto/argon2
#    também, então não há nenhum link externo para tornar "-static" —
#    a flag foi removida por ser um no-op enganoso.
#    GOOS/GOARCH vêm de TARGETOS/TARGETARCH (definidos pelo --platform
#    passado ao buildx), então o mesmo Dockerfile serve tanto pro
#    Raspberry Pi (linux/arm64) quanto pra rodar local num PC amd64.
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

# Certificados CA — o binário hoje não faz nenhuma chamada HTTPS de saída
# (os links de WhatsApp são só texto, seguidos pelo navegador do usuário),
# então isto é opcional. Mantido por ~200KB para não quebrar caso uma
# integração futura (ex.: WhatsApp Business API real) precise validar TLS
# de saída. Remova a linha se quiser espremer os últimos KBs.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Binário compilado (contém os templates embutidos, não precisa copiar .go)
COPY --from=builder /govote /govote

# Roda como não-root. scratch não tem /etc/passwd, mas um UID numérico
# funciona sem precisar de entrada nele — só garanta que o host dá
# permissão de escrita nesse UID/GID para o diretório montado em /data
# (veja o comentário no docker-compose.yaml).
USER 65532:65532

EXPOSE 9080 8443

ENTRYPOINT ["/govote"]
