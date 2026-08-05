// Command govote starts the voting API: it opens the SQLite database, ensures a
// TLS certificate exists, and runs an HTTPS server plus an HTTP→HTTPS redirector
// with graceful shutdown.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/waldirborbajr/govote/internal/server"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/web"
)

// version is set at build time via:
//   -ldflags "-X main.version=1.2.3"
var version = "dev"

const (
	httpAddr        = ":9080"
	httpsAddr       = ":8443"
	rwTimeout       = 30 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("❌ %v", err)
	}
}

func run() error {
	db := storage.MustOpen("votes.db")
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("⚠️  erro ao fechar banco de dados: %v", err)
		}
	}()

	// Se InitDB precisar da conexão aberta acima, use: storage.InitDB(db)
	if err := storage.InitDB(); err != nil {
		return fmt.Errorf("init db falhou: %w", err)
	}

	cert, err := server.EnsureSelfSignedCert()
	if err != nil {
		return fmt.Errorf("falha ao preparar certificado TLS: %w", err)
	}

	httpsSrv := &http.Server{
		Addr:         httpsAddr,
		Handler: web.Chain(
		http.HandlerFunc(server.Router),
		web.SecurityHeadersMiddleware,
		web.CSRFMiddleware,
	),
		ReadTimeout:  rwTimeout,
		WriteTimeout: rwTimeout,
		IdleTimeout:  idleTimeout,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	// HTTP: /health responde direto (útil para probes/CI sem TLS).
	// Qualquer outro path redireciona para HTTPS.
	httpSrv := &http.Server{
		Addr: httpAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok","version":"` + version + `"}`))
				return
			}
			server.HTTPSRedirectHandler(httpsAddr)(w, r)
		}),
		ReadTimeout:  rwTimeout,
		WriteTimeout: rwTimeout,
		IdleTimeout:  idleTimeout,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stop)

	errCh := make(chan error, 2)

	go func() {
		fmt.Println("🔒 Vote API (HTTPS) iniciada em https://localhost" + httpsAddr)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("servidor HTTPS: %w", err)
		}
	}()

	go func() {
		fmt.Println("↪️  Redirecionador HTTP → HTTPS em http://localhost" + httpAddr)
		fmt.Println("   /health disponível em HTTP (sem redirect) para probes.")
		fmt.Println("   Pressione Ctrl+C para encerrar gracefulmente.")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("servidor HTTP: %w", err)
		}
	}()

	select {
	case <-stop:
		fmt.Println("\n\n🛑 Sinal de shutdown recebido. Iniciando encerramento graceful...")
	case err := <-errCh:
		log.Printf("❌ erro em um dos servidores, iniciando encerramento graceful: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("⚠️  Erro durante shutdown do servidor HTTP: %v", err)
	}
	if err := httpsSrv.Shutdown(ctx); err != nil {
		log.Printf("⚠️  Erro durante shutdown do servidor HTTPS: %v", err)
	}

	fmt.Println("✅ Servidores encerrados com sucesso (todas as sessões ativas foram finalizadas)")
	return nil
}
