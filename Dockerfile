# ====================== BUILD STAGE ======================
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copiar arquivos de dependência primeiro (melhor cache)
COPY go.mod go.sum ./
RUN go mod download

# Copiar o código fonte
COPY . .

# Build estático otimizado
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o /govote ./cmd/govote

# ====================== PRODUCTION STAGE (menor possível) ======================
FROM scratch

# Copiar binário compilado
COPY --from=builder /govote /govote

# Copiar certificados TLS (necessário para HTTPS)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copiar templates e assets estáticos (se existirem)
COPY --from=builder /app/internal/web/templates.go /internal/web/
# Se você tiver pastas de templates HTML ou assets estáticos, copie aqui:
# COPY --from=builder /app/internal/web/ /internal/web/

EXPOSE 8443 8080

# O binário principal
ENTRYPOINT ["/govote"]