# ====================== BUILD STAGE ======================
FROM golang:1.23-alpine AS builder

WORKDIR /app

# 1. Copiar apenas os arquivos de dependências (melhor cache)
COPY go.mod go.sum ./
RUN go mod download

# 2. Copiar o código fonte (esta camada muda apenas quando o código muda)
COPY . .

# 3. Build estático otimizado
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o /govote ./cmd/govote

# ====================== PRODUCTION STAGE (menor possível) ======================
FROM scratch

# Copiar binário compilado
COPY --from=builder /govote /govote

# Copiar certificados TLS (para HTTPS)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copiar templates (essencial para esta aplicação)
COPY --from=builder /app/internal/web/templates.go /internal/web/templates.go

# Se no futuro você adicionar arquivos estáticos (CSS, JS, imagens), copie assim:
# COPY --from=builder /app/internal/web/static /internal/web/static

EXPOSE 8443 8080

ENTRYPOINT ["/govote"]
