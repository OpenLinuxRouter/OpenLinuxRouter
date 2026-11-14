package ingress

import (
	"strings"
	"testing"
	"time"
)

// The status text used to decide "expired" from a duration that the API does
// not serialise, so a client that decoded status over the socket always saw
// zero and printed EXPIRED on a certificate with weeks left. The expiry that
// travels is the certificate's own.
func TestStatusTextDoesNotCallAValidCertificateExpired(t *testing.T) {
	s := statusResponse{Enabled: true, Domain: "svc.example.com"}
	s.Certificate = CertState{
		Found:         true,
		Cert:          &IssuedCert{NotAfter: time.Now().Add(39 * 24 * time.Hour)},
		ExpiresInDays: 39,
	}

	var b strings.Builder
	writeCertText(&b, s)
	got := b.String()
	if strings.Contains(got, "EXPIRED") {
		t.Fatalf("a valid certificate was reported expired:\n%s", got)
	}
	if !strings.Contains(got, "valid for 39 days") {
		t.Fatalf("expected the remaining days:\n%s", got)
	}
}

func TestStatusTextReportsAnExpiredCertificate(t *testing.T) {
	s := statusResponse{Enabled: true, Domain: "svc.example.com"}
	s.Certificate = CertState{
		Found: true,
		Cert:  &IssuedCert{NotAfter: time.Now().Add(-time.Hour)},
	}

	var b strings.Builder
	writeCertText(&b, s)
	if !strings.Contains(b.String(), "EXPIRED") {
		t.Fatalf("expected EXPIRED:\n%s", b.String())
	}
}
