package ai

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
)

// Do not log the original transport error: it can contain endpoint credentials.
func transportError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("AI request canceled: %w", context.Canceled)
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return fmt.Errorf("AI request timed out: %w", context.DeadlineExceeded)
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return errors.New("AI DNS lookup failed")
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) {
		return errors.New("AI TLS certificate verification failed")
	}
	var connection *net.OpError
	if errors.As(err, &connection) {
		return errors.New("AI network connection failed")
	}
	return errors.New("AI HTTP transport failed")
}
