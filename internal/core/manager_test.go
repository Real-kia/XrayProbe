package core

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	if normalizeVersion("26.3.27") != "v26.3.27" || normalizeVersion("v26.3.27") != "v26.3.27" {
		t.Fatal("version normalization failed")
	}
}

func TestChooseAsset(t *testing.T) {
	asset, err := chooseAsset([]Asset{{Name: "Xray-linux-64.zip", BrowserURL: "x"}}, "linux", "amd64")
	if err != nil || asset.Name != "Xray-linux-64.zip" {
		t.Fatalf("unexpected asset: %+v, %v", asset, err)
	}
}

func TestSetAndCurrent(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{Cache: filepath.Join(dir, "cores")}
	if err := m.SetCurrent("v1.2.3"); err != nil {
		t.Fatal(err)
	}
	current, err := m.Current()
	if err != nil || current != "v1.2.3" {
		t.Fatalf("unexpected current core: %q, %v", current, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cores", "default-core")); err != nil {
		t.Fatal(err)
	}
}

func TestListAcceptsCurrentSizedReleaseMetadata(t *testing.T) {
	body := "[" + strings.Repeat(" ", 8<<20) + `{"tag_name":"v26.3.27","assets":[]}]`
	m := &Manager{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
		})},
		Repo: "XTLS/Xray-core", BaseURL: "https://api.example.test",
	}
	releases, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].TagName != "v26.3.27" {
		t.Fatalf("unexpected releases: %#v", releases)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func fakeCore(t *testing.T, m *Manager, version string) string {
	t.Helper()
	binary := m.binaryPath(version)
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func failingHTTP(t *testing.T) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Errorf("unexpected network request to %s", req.URL)
		return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
	})}
}

func TestEnsureUsesCachedPinnedVersionWithoutNetwork(t *testing.T) {
	m := &Manager{Cache: filepath.Join(t.TempDir(), "cores"), HTTP: failingHTTP(t), Repo: "XTLS/Xray-core", BaseURL: "https://api.example.test"}
	want := fakeCore(t, m, "v26.3.27")
	for _, requested := range []string{"v26.3.27", "26.3.27"} {
		binary, version, err := m.Ensure(context.Background(), requested)
		if err != nil || binary != want || version != "v26.3.27" {
			t.Fatalf("Ensure(%q) = %q, %q, %v", requested, binary, version, err)
		}
	}
}

func TestEnsureLatestFallsBackToInstalledCoreWhenGitHubRefuses(t *testing.T) {
	forbidden := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
	})}
	m := &Manager{Cache: filepath.Join(t.TempDir(), "cores"), HTTP: forbidden, Repo: "XTLS/Xray-core", BaseURL: "https://api.example.test"}
	fakeCore(t, m, "v25.1.1")
	want := fakeCore(t, m, "v26.3.27")
	binary, version, err := m.Ensure(context.Background(), "")
	if err != nil || binary != want || version != "v26.3.27" {
		t.Fatalf("Ensure(latest) = %q, %q, %v", binary, version, err)
	}
}

func TestEnsureUncachedPinnedVersionStillFailsWhenGitHubRefuses(t *testing.T) {
	forbidden := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
	})}
	m := &Manager{Cache: filepath.Join(t.TempDir(), "cores"), HTTP: forbidden, Repo: "XTLS/Xray-core", BaseURL: "https://api.example.test"}
	fakeCore(t, m, "v25.1.1")
	if _, _, err := m.Ensure(context.Background(), "v26.3.27"); err == nil {
		t.Fatal("expected an error for an uncached pinned version")
	}
}
