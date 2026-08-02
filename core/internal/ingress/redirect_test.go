package ingress

import (
	"errors"
	"net"
	"testing"

	"UAmask/internal/config"
)

func TestRedirectCloseIsIdempotentAndStopsAccept(t *testing.T) {
	redirect, err := NewRedirect(config.ListenConfig{})
	if err != nil {
		t.Fatalf("LIFE-INGRESS-201 NewRedirect: %v", err)
	}
	if err := redirect.Close(); err != nil {
		t.Fatalf("LIFE-INGRESS-201 first Close: %v", err)
	}
	if err := redirect.Close(); err != nil {
		t.Fatalf("LIFE-INGRESS-201 second Close: %v", err)
	}
	if _, err := redirect.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("LIFE-INGRESS-201 Accept error = %v, want net.ErrClosed", err)
	}

	var nilRedirect *Redirect
	if err := nilRedirect.Close(); err != nil {
		t.Fatalf("LIFE-INGRESS-201 nil Close: %v", err)
	}
}
