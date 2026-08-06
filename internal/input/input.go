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

type Source struct {
	Value string
	Kind  string
}

type LoadOptions struct {
	MaxConfigs    int
	AllowedRoots  []string
	RestrictPaths bool
}

var supportedSchemes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "ss": true,
}

func Load(ctx context.Context, source string, maxConfigs int) ([]types.Spec, error) {
	return LoadSource(ctx, Source{Value: source, Kind: "auto"}, LoadOptions{MaxConfigs: maxConfigs})
}

func ReadFile(path string, options LoadOptions) ([]byte, error) {
	return readPath(path, options)
}

func LoadSource(ctx context.Context, source Source, options LoadOptions) ([]types.Spec, error) {
	maxConfigs := options.MaxConfigs
	if maxConfigs <= 0 {
		maxConfigs = 500
	}
	trimmed := strings.TrimSpace(source.Value)
	if len(trimmed) > maxSubscriptionBytes {
		return nil, errors.New("input exceeds 10 MiB limit")
	}
	kind := strings.ToLower(strings.TrimSpace(source.Kind))
	if kind == "" {
		kind = "auto"
	}
	switch kind {
	case "uri":
		if !isURI(trimmed) {
			return nil, errors.New("source is not a supported Xray share link")
		}
		return []types.Spec{{Index: 0, Name: uriName(trimmed, 1), Source: "argument", Kind: "uri", URI: trimmed}}, nil
	case "json", "text", "subscription_text":
		return fromContent([]byte(trimmed), "argument", maxConfigs)
	case "url", "subscription_url":
		body, err := fetch(ctx, trimmed)
		if err != nil {
			return nil, err
		}
		return fromContent(body, trimmed, maxConfigs)
	case "path":
		b, err := readPath(trimmed, options)
		if err != nil {
			return nil, err
		}
		return fromContent(b, trimmed, maxConfigs)
	case "auto":
		if isURI(trimmed) {
			return []types.Spec{{Index: 0, Name: uriName(trimmed, 1), Source: "argument", Kind: "uri", URI: trimmed}}, nil
		}
		if looksLikeJSON(trimmed) {
			return fromContent([]byte(trimmed), "argument", maxConfigs)
		}
		if u, err := url.Parse(trimmed); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			body, err := fetch(ctx, trimmed)
			if err != nil {
				return nil, err
			}
			return fromContent(body, trimmed, maxConfigs)
		}
		b, err := readPath(trimmed, options)
		if err != nil {
			return nil, err
		}
		return fromContent(b, trimmed, maxConfigs)
	default:
		return nil, fmt.Errorf("unsupported source_type %q", source.Kind)
	}
}

func readPath(path string, options LoadOptions) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("file path is empty")
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("resolve input path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve input path: %w", err)
	}
	if options.RestrictPaths {
		allowed, err := canonicalRoots(options.AllowedRoots)
		if err != nil {
			return nil, err
		}
		permitted := false
		for _, root := range allowed {
			if within(root, canonical) {
				permitted = true
				break
			}
		}
		if !permitted {
			return nil, errors.New("input path is outside the MCP allowed roots")
		}
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input path is not a regular file")
	}
	file, err := os.Open(canonical)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, maxSubscriptionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	if len(b) > maxSubscriptionBytes {
		return nil, errors.New("input file exceeds 10 MiB limit")
	}
	return b, nil
}

func canonicalRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve MCP working directory: %w", err)
		}
		roots = []string{cwd}
	}
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		absolute, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			return nil, fmt.Errorf("resolve allowed path: %w", err)
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve allowed path: %w", err)
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("allowed path is not a directory: %s", root)
		}
		result = append(result, canonical)
	}
	return result, nil
}

func NormalizeAllowedRoots(roots []string) ([]string, error) {
	return canonicalRoots(roots)
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
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
	req.Header.Set("User-Agent", "xrayprobe/0.2")
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
