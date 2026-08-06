package xray

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KiaTheRandomGuy/XrayProbe/internal/types"
)

func TestBuildVLESSReality(t *testing.T) {
	spec := types.Spec{Kind: "uri", URI: "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=reality&sni=example.org&fp=chrome&pbk=public&sid=abcd&type=ws&path=%2Fedge#demo"}
	b, metadata, err := Build(spec, 18080, "")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Protocol != "vless" || metadata.Transport != "ws" || metadata.Security != "reality" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	var config map[string]any
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	if len(config["inbounds"].([]any)) != 1 {
		t.Fatal("expected one generated inbound")
	}
	if !strings.Contains(string(b), "xrayprobe-target") {
		t.Fatal("expected target tag")
	}
}

func TestBuildJSONSelectsProxyOutbound(t *testing.T) {
	spec := types.Spec{Kind: "json", Config: map[string]any{"outbounds": []any{map[string]any{"protocol": "freedom", "tag": "direct"}, map[string]any{"protocol": "vless", "tag": "proxy"}}}}
	b, metadata, err := Build(spec, 18081, "proxy")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Protocol != "vless" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	var config map[string]any
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	if len(config["inbounds"].([]any)) != 1 {
		t.Fatal("expected generated inbound")
	}
	routing := config["routing"].(map[string]any)
	if len(routing["rules"].([]any)) != 1 {
		t.Fatal("expected generated routing rule")
	}
}

func TestBuildRejectsMissingOutbound(t *testing.T) {
	_, _, err := Build(types.Spec{Kind: "json", Config: map[string]any{}}, 18082, "")
	if err == nil {
		t.Fatal("expected missing outbound error")
	}
}
