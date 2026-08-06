package input

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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

func TestLoadSourceRestrictsPathsToAllowedRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	inside := filepath.Join(root, "config.txt")
	outsidePath := filepath.Join(outside, "config.txt")
	content := []byte("vless://a@example.com:443")
	if err := os.WriteFile(inside, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSource(context.Background(), Source{Value: inside, Kind: "path"}, LoadOptions{MaxConfigs: 10, AllowedRoots: []string{root}, RestrictPaths: true}); err != nil {
		t.Fatalf("allowed path rejected: %v", err)
	}
	if _, err := LoadSource(context.Background(), Source{Value: outsidePath, Kind: "path"}, LoadOptions{MaxConfigs: 10, AllowedRoots: []string{root}, RestrictPaths: true}); err == nil {
		t.Fatal("expected path outside the allowlist to be rejected")
	}
}

func TestLoadSourceRejectsOversizedDirectInput(t *testing.T) {
	oversized := make([]byte, maxSubscriptionBytes+1)
	if _, err := LoadSource(context.Background(), Source{Value: string(oversized), Kind: "text"}, LoadOptions{MaxConfigs: 10}); err == nil {
		t.Fatal("expected oversized direct input to be rejected")
	}
}
