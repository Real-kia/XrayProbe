package input

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KiaTheRandomGuy/XrayProbe/internal/types"
)

const maxSubscriptionBytes = 10 << 20

var supportedSchemes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "ss": true,
}

func Load(ctx context.Context, source string, maxConfigs int) ([]types.Spec, error) {
	if maxConfigs <= 0 {
		maxConfigs = 500
	}
	trimmed := strings.TrimSpace(source)
	if isURI(trimmed) {
		return []types.Spec{{Index: 0, Name: uriName(trimmed, 1), Source: "argument", Kind: "uri", URI: trimmed}}, nil
	}
	if u, err := url.Parse(trimmed); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		body, err := fetch(ctx, trimmed)
		if err != nil {
			return nil, err
		}
		return fromContent(body, trimmed, maxConfigs)
	}
	b, err := os.ReadFile(filepath.Clean(trimmed))
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	return fromContent(b, trimmed, maxConfigs)
}

func fromContent(data []byte, source string, maxConfigs int) ([]types.Spec, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, errors.New("input is empty")
	}
	if looksLikeJSON(trimmed) {
		var config map[string]any
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("parse JSON config: %w", err)
		}
		return []types.Spec{{Index: 0, Name: filepath.Base(source), Source: source, Kind: "json", Config: config}}, nil
	}
	lines := splitLinks(trimmed)
	if len(lines) == 0 {
		if decoded, ok := decodeSubscription(trimmed); ok {
			lines = splitLinks(decoded)
		}
	}
	if len(lines) == 0 {
		return nil, errors.New("input is neither an Xray JSON config nor a supported subscription")
	}
	if len(lines) > maxConfigs {
		return nil, fmt.Errorf("subscription contains %d configs; limit is %d", len(lines), maxConfigs)
	}
	seen := make(map[string]bool)
	result := make([]types.Spec, 0, len(lines))
	for _, line := range lines {
		if !isURI(line) || seen[line] {
			continue
		}
		seen[line] = true
		result = append(result, types.Spec{Index: len(result), Name: uriName(line, len(result)+1), Source: source, Kind: "uri", URI: line})
	}
	if len(result) == 0 {
		return nil, errors.New("subscription has no supported VLESS, VMess, Trojan, or Shadowsocks links")
	}
	return result, nil
}

func fetch(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" {
		return nil, errors.New("remote inputs must use HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xrayprobe/0.1")
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("subscription redirects must remain HTTPS")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("subscription returned %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSubscriptionBytes {
		return nil, errors.New("subscription exceeds 10 MiB limit")
	}
	return b, nil
}

func splitLinks(content string) []string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if isURI(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

func decodeSubscription(content string) (string, bool) {
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, content)
	decoders := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	for _, decoder := range decoders {
		b, err := decoder.DecodeString(compact)
		if err == nil && strings.Contains(strings.ToLower(string(b)), "://") {
			return string(b), true
		}
	}
	return "", false
}

func isURI(value string) bool {
	idx := strings.Index(value, "://")
	if idx <= 0 {
		return false
	}
	return supportedSchemes[strings.ToLower(value[:idx])]
}

func looksLikeJSON(value string) bool {
	return strings.HasPrefix(value, "{")
}

func uriName(raw string, index int) string {
	u, err := url.Parse(raw)
	if err == nil && u.Fragment != "" {
		return u.Fragment
	}
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return fmt.Sprintf("config-%d", index)
}
