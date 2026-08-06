package core

import (
	"os"
	"path/filepath"
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
