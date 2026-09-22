package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"time"
)

// TLSConfig returns secure defaults for the server's TLS configuration. When
// SSL is enabled without certificate files, it supplies an ephemeral
// self-signed certificate so the HTTPS server can start without external files.
func (c Config) TLSConfig() (*tls.Config, error) {
	config := &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
	}
	if c.SSLEnabled && c.TLSCertFile == "" && c.TLSKeyFile == "" {
		certificate, err := defaultCertificate()
		if err != nil {
			return nil, err
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func defaultCertificate() (tls.Certificate, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  privateKey,
	}, nil
}
