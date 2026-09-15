package remote

import (
	"strings"
	"testing"
	"time"

	"github.com/Real-kia/XrayProbe/internal/version"
)

func TestParseAndValidateTargets(t *testing.T) {
	targets, err := ParseTargets([]string{"iran=root@192.0.2.10", "de=probe-de"})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(targets, time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.Names(), ","); got != "de,iran" {
		t.Fatalf("target names = %q", got)
	}

	for _, value := range []string{"missing-equals", "=root@host", "bad name=root@host", "local=root@host", "x=-oProxyCommand=bad"} {
		parsed, parseErr := ParseTargets([]string{value})
		if parseErr == nil {
			if _, parseErr = New(parsed, time.Minute, false); parseErr == nil {
				t.Errorf("expected target %q to be rejected", value)
			}
		}
	}
}

func TestDynamicTargetUsesDirectSSHAddress(t *testing.T) {
	runner, err := New(nil, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	target, err := runner.target("root@192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if target.Address != "root@192.0.2.10" {
		t.Fatalf("dynamic target address = %q", target.Address)
	}
	if _, err := runner.target("bad target"); err == nil {
		t.Fatal("expected whitespace in dynamic SSH target to be rejected")
	}
}

func TestBootstrapScriptPinsCurrentRelease(t *testing.T) {
	script := bootstrapScript()
	if !strings.Contains(script, "XRAYPROBE_VERSION=v"+version.Value) || !strings.Contains(script, "exec \"$bin\" remote-worker") {
		t.Fatalf("bootstrap script does not pin the worker release:\n%s", script)
	}
}
