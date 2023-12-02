package redis

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func getTLSConfig(CAPath string, host string) (*tls.Config, error) {
	c := &tls.Config{}
	if host == localhost {
		c.InsecureSkipVerify = true
	}
	if CAPath != "" {
		cert, err := os.ReadFile(CAPath)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		ok := pool.AppendCertsFromPEM(cert)
		if !ok {
			return nil, fmt.Errorf("unable to build cert pool from pem at %s", CAPath)
		}
		c.RootCAs = pool
	}
	return c, nil
}
