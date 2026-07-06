package security

import "testing"

func TestDefaultConsentDeny(t *testing.T) {
	if DefaultConsentAction() != ConsentDeny {
		t.Fatal("consent must default deny")
	}
}

func TestHoneypotSkipInvisible(t *testing.T) {
	d := ClassifyHoneypot(map[string]string{"style": "display:none", "name": "email"})
	if !d.Skip {
		t.Fatal("invisible honeypot must skip")
	}
}

func TestLoginFlowDetect(t *testing.T) {
	if !DetectLoginFlow("Sign in to continue") {
		t.Fatal("login flow expected")
	}
}
