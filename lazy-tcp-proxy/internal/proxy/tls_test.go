package proxy

import (
	"crypto/ecdsa"
	"crypto/x509"
	"testing"
	"time"
)

func TestGenerateSelfSignedTLSConfig(t *testing.T) {
	cfg, err := GenerateSelfSignedTLSConfig()
	if err != nil {
		t.Fatalf("GenerateSelfSignedTLSConfig() error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil *tls.Config")
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(cfg.Certificates))
	}

	cert := cfg.Certificates[0]
	if _, ok := cert.PrivateKey.(*ecdsa.PrivateKey); !ok {
		t.Error("expected ECDSA private key")
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	if time.Until(x509Cert.NotAfter) < 9*365*24*time.Hour {
		t.Errorf("certificate expiry too soon: %s", x509Cert.NotAfter)
	}
}
