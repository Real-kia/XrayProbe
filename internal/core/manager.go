package core

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	defaultRepo   = "XTLS/Xray-core"
	latestVersion = "latest"
)

type Manager struct {
	HTTP    *http.Client
	Repo    string
	Cache   string
	BaseURL string
}

type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

type Asset struct {
	Name       string `json:"name"`
	BrowserURL string `json:"browser_download_url"`
	Digest     string `json:"digest"`
}

func NewManager() (*Manager, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return &Manager{
		HTTP:    &http.Client{Timeout: 45 * time.Second},
		Repo:    defaultRepo,
		Cache:   filepath.Join(cache, "xrayprobe", "cores"),
		BaseURL: "https://api.github.com",
	}, nil
}

func (m *Manager) currentFile() string {
	return filepath.Join(m.Cache, "default-core")
}

func (m *Manager) Current() (string, error) {
	b, err := os.ReadFile(m.currentFile())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return latestVersion, nil
		}
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return latestVersion, nil
	}
	return v, nil
}

func (m *Manager) SetCurrent(version string) error {
	if version == "" {
		return errors.New("core version cannot be empty")
	}
	if err := os.MkdirAll(m.Cache, 0o700); err != nil {
		return err
	}
	tmp := m.currentFile() + ".tmp"
	if err := os.WriteFile(tmp, []byte(version+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.currentFile())
}

func (m *Manager) List(ctx context.Context) ([]Release, error) {
	url := strings.TrimRight(m.BaseURL, "/") + "/repos/" + m.Repo + "/releases?per_page=100"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub release API returned %s", resp.Status)
	}
	var releases []Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (m *Manager) Resolve(ctx context.Context, requested string) (Release, error) {
	releases, err := m.List(ctx)
	if err != nil {
		return Release{}, err
	}
	if requested == "" || requested == latestVersion {
		for _, release := range releases {
			if !release.Draft && !release.Prerelease {
				return release, nil
			}
		}
		return Release{}, errors.New("no stable Xray-core release was found")
	}
	requested = normalizeVersion(requested)
	for _, release := range releases {
		if normalizeVersion(release.TagName) == requested {
			return release, nil
		}
	}
	return Release{}, fmt.Errorf("Xray-core release %q was not found", requested)
}

func (m *Manager) Ensure(ctx context.Context, requested string) (string, string, error) {
	if requested == "" {
		requested, _ = m.Current()
	}
	release, err := m.Resolve(ctx, requested)
	if err != nil {
		return "", "", err
	}
	asset, err := chooseAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", "", err
	}
	version := normalizeVersion(release.TagName)
	dir := filepath.Join(m.Cache, version, runtime.GOOS+"-"+runtime.GOARCH)
	binary := filepath.Join(dir, "xray")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if executable(binary) {
		return binary, version, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	archivePath := filepath.Join(dir, asset.Name)
	if err := m.download(ctx, asset.BrowserURL, archivePath); err != nil {
		return "", "", err
	}
	digest, err := m.assetDigest(ctx, release.Assets, asset, dir)
	if err != nil {
		return "", "", err
	}
	if err := verifyFile(archivePath, digest); err != nil {
		return "", "", err
	}
	if err := extractBinary(archivePath, binary, dir); err != nil {
		return "", "", err
	}
	return binary, version, nil
}

func (m *Manager) assetDigest(ctx context.Context, assets []Asset, asset Asset, dir string) (string, error) {
	if asset.Digest != "" {
		return asset.Digest, nil
	}
	var digestAsset *Asset
	for i := range assets {
		if assets[i].Name == asset.Name+".dgst" {
			digestAsset = &assets[i]
			break
		}
	}
	if digestAsset == nil {
		return "", errors.New("release asset has no SHA-256 digest")
	}
	path := filepath.Join(dir, digestAsset.Name)
	if err := m.download(ctx, digestAsset.BrowserURL, path); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	match := regexp.MustCompile(`(?i)sha256:\s*([a-f0-9]{64})|\b([a-f0-9]{64})\b`).FindSubmatch(b)
	if len(match) == 0 {
		return "", errors.New("release digest sidecar has no SHA-256 value")
	}
	for _, group := range match[1:] {
		if len(group) > 0 {
			return "sha256:" + string(group), nil
		}
	}
	return "", errors.New("release digest sidecar has no SHA-256 value")
}

func (m *Manager) download(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core download returned %s", resp.Status)
	}
	tmp := path + ".download"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, io.LimitReader(resp.Body, 100<<20))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}

func (m *Manager) Installed() ([]string, error) {
	entries, err := os.ReadDir(m.Cache)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "v") {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func chooseAsset(assets []Asset, goos, goarch string) (Asset, error) {
	var candidates []string
	switch goos + "/" + goarch {
	case "linux/amd64":
		candidates = []string{"Xray-linux-64.zip"}
	case "linux/arm64":
		candidates = []string{"Xray-linux-arm64-v8a.zip", "Xray-linux-arm64.zip"}
	case "darwin/amd64":
		candidates = []string{"Xray-macos-64.zip", "Xray-darwin-64.zip"}
	case "darwin/arm64":
		candidates = []string{"Xray-macos-arm64-v8a.zip", "Xray-macos-arm64.zip", "Xray-darwin-arm64-v8a.zip"}
	case "windows/amd64":
		candidates = []string{"Xray-windows-64.zip"}
	case "windows/arm64":
		candidates = []string{"Xray-windows-arm64-v8a.zip", "Xray-windows-arm64.zip"}
	default:
		return Asset{}, fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
	for _, candidate := range candidates {
		for _, asset := range assets {
			if asset.Name == candidate {
				return asset, nil
			}
		}
	}
	return Asset{}, fmt.Errorf("Xray-core has no release asset for %s/%s", goos, goarch)
}

func extractBinary(archivePath, binaryPath, dir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	foundBinary := false
	for _, file := range r.File {
		base := filepath.Base(file.Name)
		if base == "" || base == "." || (base != "xray" && base != "xray.exe" && !strings.HasSuffix(base, ".dat")) {
			continue
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		target := filepath.Join(dir, base)
		if base == "xray" || base == "xray.exe" {
			target = binaryPath
		}
		out, err := os.OpenFile(target+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if runtime.GOOS != "windows" && (base == "xray" || base == "xray.exe") {
			if err := os.Chmod(target+".tmp", 0o700); err != nil {
				return err
			}
		}
		if err := os.Rename(target+".tmp", target); err != nil {
			return err
		}
		if base == "xray" || base == "xray.exe" {
			foundBinary = true
		}
	}
	if !foundBinary {
		return errors.New("Xray binary was not found in release archive")
	}
	return nil
}

func verifyFile(path, digest string) error {
	if digest == "" {
		return errors.New("release asset has no SHA-256 digest")
	}
	expected := strings.TrimPrefix(strings.TrimSpace(digest), "sha256:")
	if !regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(expected) {
		return errors.New("invalid release asset digest")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("core checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o100 != 0
}

func normalizeVersion(v string) string {
	if v == "" || v == latestVersion {
		return v
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
