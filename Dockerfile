# ====================== BUILD STAGE ======================
# go.mod exige "go 1.26" — usar imagem mais antiga faz o build falhar
# com "go.mod requires go >= 1.26".
FROM golang:1.26-alpine AS builder

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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
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

EXPOSE 8080 8443

ENTRYPOINT ["/govote"]
