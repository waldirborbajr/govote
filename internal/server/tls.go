package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	tlsCertFile = "cert.pem"
	tlsKeyFile  = "key.pem"
)

// EnsureSelfSignedCert carrega cert.pem/key.pem do disco se existirem,
// ou gera um novo par autoassinado (válido para localhost/127.0.0.1) caso contrário.
func EnsureSelfSignedCert() (tls.Certificate, error) {
	if cert, err := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile); err == nil {
		return cert, nil
	}

	fmt.Println("🔑 Certificado não encontrado, gerando novo certificado autoassinado...")

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("erro ao gerar chave privada: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("erro ao gerar número de série: %w", err)
	}

	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Sistema de Votação - Dev Local"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &certTemplate, &certTemplate, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("erro ao criar certificado: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("erro ao serializar chave privada: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	if err := os.WriteFile(tlsCertFile, certPEM, 0644); err != nil {
		return tls.Certificate{}, fmt.Errorf("erro ao salvar %s: %w", tlsCertFile, err)
	}
	if err := os.WriteFile(tlsKeyFile, keyPEM, 0600); err != nil {
		return tls.Certificate{}, fmt.Errorf("erro ao salvar %s: %w", tlsKeyFile, err)
	}

	fmt.Printf("✅ Certificado autoassinado gerado: %s / %s (válido 10 anos)\n", tlsCertFile, tlsKeyFile)

	return tls.X509KeyPair(certPEM, keyPEM)
}

// HTTPSRedirectHandler responde a qualquer requisição HTTP redirecionando
// para a mesma URL em HTTPS, na porta configurada.
func HTTPSRedirectHandler(httpsPort string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		target := "https://" + host + httpsPort + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	}
}
