#!/bin/sh
set -e

CERT_DIR="/certs"
CA_KEY="$CERT_DIR/ca.key"
CA_CERT="$CERT_DIR/ca.crt"
SERVER_KEY="$CERT_DIR/server.key"
SERVER_CERT="$CERT_DIR/server.crt"
SERVER_CSR="$CERT_DIR/server.csr"

# Skip generation if certs already exist and are valid
if [ -f "$SERVER_CERT" ] && [ -f "$SERVER_KEY" ] && [ -f "$CA_CERT" ]; then
  # Check if the server cert expires in more than 30 days
  if openssl x509 -checkend 2592000 -noout -in "$SERVER_CERT" 2>/dev/null; then
    echo "SSL certificates already exist and are valid. Skipping generation."
    exit 0
  fi
  echo "SSL certificates exist but are expiring soon. Regenerating..."
fi

echo "Generating self-signed SSL certificates for PostgreSQL..."

# 1. Generate CA private key and self-signed certificate
openssl req -new -x509 -days 3650 -nodes \
  -keyout "$CA_KEY" \
  -out "$CA_CERT" \
  -subj "/CN=MAP Local CA/O=MAP Development/C=AU"

# 2. Generate server private key
openssl genrsa -out "$SERVER_KEY" 2048

# 3. Generate server certificate signing request
openssl req -new \
  -key "$SERVER_KEY" \
  -out "$SERVER_CSR" \
  -subj "/CN=postgres/O=MAP Development/C=AU"

# 4. Sign the server certificate with our CA
openssl x509 -req -days 3650 \
  -in "$SERVER_CSR" \
  -CA "$CA_CERT" \
  -CAkey "$CA_KEY" \
  -CAcreateserial \
  -out "$SERVER_CERT" \
  -extfile /dev/stdin <<EOF
subjectAltName = DNS:postgres, DNS:localhost, IP:127.0.0.1
EOF

# 5. Set permissions (PostgreSQL requires strict permissions on the key)
chmod 600 "$SERVER_KEY"
chmod 644 "$SERVER_CERT" "$CA_CERT"

# 6. Change ownership to postgres user (uid 70 in alpine postgres image)
chown 70:70 "$SERVER_KEY" "$SERVER_CERT" "$CA_CERT"

# 7. Clean up intermediate files
rm -f "$SERVER_CSR" "$CERT_DIR/ca.srl"

echo "SSL certificates generated successfully in $CERT_DIR"
echo "  CA certificate:     $CA_CERT"
echo "  Server certificate: $SERVER_CERT"
echo "  Server key:         $SERVER_KEY"
