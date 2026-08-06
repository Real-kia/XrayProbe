package input

import "testing"

func TestDecodeSubscription(t *testing.T) {
	encoded := "dmxlc3M6Ly9hYmMxMjNAZXhhbXBsZS5jb206NDQzP3NlY3VyaXR5PXRscyN0ZXN0"
	decoded, ok := decodeSubscription(encoded)
	if !ok || decoded == "" || decoded[:8] != "vless://" {
		t.Fatalf("unexpected decoded subscription: %q, %v", decoded, ok)
	}
}

func TestFromContentDeduplicatesLinks(t *testing.T) {
	content := "vless://a@example.com:443\nvless://a@example.com:443\n# comment\n"
	specs, err := fromContent([]byte(content), "local.txt", 10)
	if err != nil || len(specs) != 1 {
		t.Fatalf("unexpected specs: %#v, %v", specs, err)
	}
}

func TestFromContentRejectsUnsupported(t *testing.T) {
	if _, err := fromContent([]byte("http://example.com"), "local.txt", 10); err == nil {
		t.Fatal("expected unsupported input error")
	}
}
