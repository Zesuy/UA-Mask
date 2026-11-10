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

// TestRedirectListenBindsBeforeFirstAccept guards the readiness contract: the
// listening socket must exist as soon as Listen returns, not only after the
// first client connects. External supervisors gate the REDIRECT rule on
// "is the port listening", so a lazy bind deadlocks against them.
func TestRedirectListenBindsBeforeFirstAccept(t *testing.T) {
	redirect, err := NewRedirect(config.ListenConfig{})
	if err != nil {
		t.Fatalf("LIFE-INGRESS-202 NewRedirect: %v", err)
	}
	defer redirect.Close()

	if redirect.listener != nil {
		t.Fatalf("LIFE-INGRESS-202 listener bound before Listen")
	}

	if err := redirect.Listen(); err != nil {
		t.Fatalf("LIFE-INGRESS-202 Listen: %v", err)
	}
	if redirect.listener == nil {
		t.Fatalf("LIFE-INGRESS-202 Listen returned without binding a listener")
	}
	first := redirect.listener.Addr().(*net.TCPAddr)
	if first.Port == 0 {
		t.Fatalf("LIFE-INGRESS-202 unexpected ephemeral port 0")
	}

	// Listen is idempotent: a second call must reuse the bound socket instead
	// of failing with EADDRINUSE.
	if err := redirect.Listen(); err != nil {
		t.Fatalf("LIFE-INGRESS-202 second Listen: %v", err)
	}
	second := redirect.listener.Addr().(*net.TCPAddr)
	if second.Port != first.Port {
		t.Fatalf("LIFE-INGRESS-202 port changed %d -> %d", first.Port, second.Port)
	}

	if err := redirect.Close(); err != nil {
		t.Fatalf("LIFE-INGRESS-202 Close: %v", err)
	}
	if err := redirect.Listen(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("LIFE-INGRESS-202 Listen after Close = %v, want net.ErrClosed", err)
	}
}
