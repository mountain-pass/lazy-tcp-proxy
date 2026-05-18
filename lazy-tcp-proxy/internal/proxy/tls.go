package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"time"
)

// GenerateSelfSignedTLSConfig generates an in-memory ECDSA P-256 self-signed
// certificate valid for 10 years and returns a *tls.Config that presents it.
func GenerateSelfSignedTLSConfig() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	now := time.Now()
	expiry := now.Add(10 * 365 * 24 * time.Hour)
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "lazy-tcp-proxy"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              expiry,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
	log.Printf("proxy: self-signed certificate generated, expires %s", expiry.Format("2006-01-02"))
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}
