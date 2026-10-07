package xray

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Real-kia/XrayProbe/internal/types"
)

func TestBuildVLESSReality(t *testing.T) {
	spec := types.Spec{Kind: "uri", URI: "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=reality&sni=example.org&fp=chrome&pbk=public&sid=abcd&type=ws&path=%2Fedge#demo"}
	b, metadata, err := Build(spec, 18080, "", "en0")
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
	var outbound map[string]any
	for _, raw := range config["outbounds"].([]any) {
		outbound = raw.(map[string]any)
	}
	streamSettings := outbound["streamSettings"].(map[string]any)
	sockopt := streamSettings["sockopt"].(map[string]any)
	if sockopt["interface"] != "en0" {
		t.Fatalf("interface = %#v, want en0", sockopt["interface"])
	}
}

func TestBuildJSONSelectsProxyOutbound(t *testing.T) {
	spec := types.Spec{Kind: "json", Config: map[string]any{"outbounds": []any{map[string]any{"protocol": "freedom", "tag": "direct"}, map[string]any{"protocol": "vless", "tag": "proxy", "streamSettings": map[string]any{"sockopt": map[string]any{"tcpFastOpen": true}}}}}}
	b, metadata, err := Build(spec, 18081, "proxy", "en1")
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
	outbound := config["outbounds"].([]any)[1].(map[string]any)
	streamSettings := outbound["streamSettings"].(map[string]any)
	sockopt := streamSettings["sockopt"].(map[string]any)
	if sockopt["interface"] != "en1" {
		t.Fatalf("interface = %#v, want en1", sockopt["interface"])
	}
	if sockopt["tcpFastOpen"] != true {
		t.Fatalf("existing sockopt was not preserved: %#v", sockopt)
	}
}

func TestBuildRejectsMissingOutbound(t *testing.T) {
	_, _, err := Build(types.Spec{Kind: "json", Config: map[string]any{}}, 18082, "", "")
	if err == nil {
		t.Fatal("expected missing outbound error")
	}
}

func TestBuildVLESSPinnedCertAndXHTTPExtra(t *testing.T) {
	rawURI := "vless://cc495702-32ed-47cb-b64d-de603ddccbc6@nice.iranapplecenter.com:443?encryption=none&security=tls&type=xhttp&headerType=none&host=vsl-pu-48731-1786406791.global.ssl.fastly.net&mode=packet-up&extra=%7B%22scMaxEachPostBytes%22%3A%221000000%22%2C%22scMinPostsIntervalMs%22%3A%2230%22%2C%22xPaddingBytes%22%3A%22100-1000%22%7D&sni=speedtest.net&fp=firefox&pcs=CD:6E:83:8B:9B:FE:31:CA:B8:B3:D5:C8:58:B0:D6:22:3F:E8:4E:CE:23:6C:1F:DB:28:1B:E9:3C:30:2E:1E:6E&alpn=h2#test"
	b, metadata, err := Build(types.Spec{Kind: "uri", URI: rawURI}, 18083, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Protocol != "vless" || metadata.Transport != "xhttp" || metadata.Security != "tls" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	var config map[string]any
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	var outbound map[string]any
	for _, raw := range config["outbounds"].([]any) {
		outbound = raw.(map[string]any)
	}
	streamSettings := outbound["streamSettings"].(map[string]any)
	tlsSettings := streamSettings["tlsSettings"].(map[string]any)
	expectedPin := "CD6E838B9BFE31CAB8B3D5C858B0D6223FE84ECE236C1FDB281BE93C302E1E6E"
	if tlsSettings["pinnedPeerCertSha256"] != expectedPin {
		t.Fatalf("pinnedPeerCertSha256 = %#v, want %#v", tlsSettings["pinnedPeerCertSha256"], expectedPin)
	}
	if tlsSettings["serverName"] != "speedtest.net" {
		t.Fatalf("serverName = %#v, want speedtest.net", tlsSettings["serverName"])
	}
	xhttpSettings := streamSettings["xhttpSettings"].(map[string]any)
	if xhttpSettings["mode"] != "packet-up" {
		t.Fatalf("mode = %#v, want packet-up", xhttpSettings["mode"])
	}
	if xhttpSettings["host"] != "vsl-pu-48731-1786406791.global.ssl.fastly.net" {
		t.Fatalf("host = %#v, want vsl-pu-48731-1786406791.global.ssl.fastly.net", xhttpSettings["host"])
	}
	extra, ok := xhttpSettings["extra"].(map[string]any)
	if !ok {
		t.Fatalf("extra = %#v, expected map", xhttpSettings["extra"])
	}
	if extra["scMaxEachPostBytes"] != "1000000" {
		t.Fatalf("extra.scMaxEachPostBytes = %#v, want 1000000", extra["scMaxEachPostBytes"])
	}
}
