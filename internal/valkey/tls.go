package valkey

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/yandex/rdsync/internal/config"
)

func getTLSConfig(config *config.Config, CAPath, host string) (*tls.Config, error) {
	c := &tls.Config{}
	if host == localhost {
		c.ServerName = config.Hostname
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
