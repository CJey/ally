package internal

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"time"
)

func autoGenerateCert() *tls.Certificate {
	curve := elliptic.P384()
	key, _ := ecdsa.GenerateKey(curve, rand.Reader)
	now := time.Now()
	keyb, _ := x509.MarshalECPrivateKey(key)
	csr := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Issuer:       pkix.Name{},
		Subject:      pkix.Name{CommonName: "ally"},
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour * 24 * 365 * 20),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certb, _ := x509.CreateCertificate(rand.Reader, &csr, &csr, &key.PublicKey, key)
	certpem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certb})
	keypem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyb})
	cert, _ := tls.X509KeyPair(certpem, keypem)
	return &cert
}

func TLSClientConn(raw *net.UnixConn) *tls.Conn {
	var cfg = &tls.Config{
		InsecureSkipVerify: true,
	}
	return tls.Client(raw, cfg)
}

func TLSServerConn(raw *net.UnixConn) *tls.Conn {
	var cert = autoGenerateCert()
	var cfg = &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	return tls.Server(raw, cfg)
}
