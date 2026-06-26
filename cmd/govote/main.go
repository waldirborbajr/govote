// Command govote starts the voting API: it opens the SQLite database, ensures a
// TLS certificate exists, and runs an HTTPS server plus an HTTP→HTTPS redirector
// with graceful shutdown.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/waldirborbajr/govote/internal/server"
	"github.com/waldirborbajr/govote/internal/storage"
)

func main() {
	db := storage.MustOpen("votes.db")
	defer db.Close()

	if err := storage.InitDB(); err != nil {
		log.Fatalf("init db failed: %v", err)
	}

	cert, err := server.EnsureSelfSignedCert()
	if err != nil {
		log.Fatalf("falha ao preparar certificado TLS: %v", err)
	}

	const (
		httpAddr  = ":8080"
		httpsAddr = ":8443"
		httpsPort = ":8443" // usado para montar a URL de redirecionamento
	)

	httpsSrv := &http.Server{
		Addr:         httpsAddr,
		Handler:      http.HandlerFunc(server.Router),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	httpSrv := &http.Server{
		Addr:         httpAddr,
		Handler:      server.HTTPSRedirectHandler(httpsPort),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		fmt.Println("🔒 Vote API (HTTPS) iniciada em https://localhost" + httpsAddr)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Erro no servidor HTTPS: %v", err)
		}
	}()

	go func() {
		fmt.Println("↪️  Redirecionador HTTP → HTTPS em http://localhost" + httpAddr)
		fmt.Println("   Pressione Ctrl+C para encerrar gracefulmente.")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Erro no servidor HTTP: %v", err)
		}
	}()

	<-stop
	fmt.Println("\n\n🛑 Sinal de shutdown recebido. Iniciando encerramento graceful...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("⚠️  Erro durante shutdown do servidor HTTP: %v", err)
	}
	if err := httpsSrv.Shutdown(ctx); err != nil {
		log.Printf("⚠️  Erro durante shutdown do servidor HTTPS: %v", err)
	}
	fmt.Println("✅ Servidores encerrados com sucesso (todas as sessões ativas foram finalizadas)")

	fmt.Println("💾 Banco de dados fechado.")
}
