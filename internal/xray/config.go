package xray

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/KiaTheRandomGuy/XrayProbe/internal/types"
)

type Metadata struct {
	Protocol  string
	Transport string
	Security  string
}

func Build(spec types.Spec, port int, requestedOutbound, networkInterface string) ([]byte, Metadata, error) {
	if spec.Kind == "json" {
		return buildJSON(spec.Config, port, requestedOutbound, networkInterface)
	}
	u, err := url.Parse(spec.URI)
	if err != nil {
		return nil, Metadata{}, fmt.Errorf("parse share link: %w", err)
	}
	var outbound map[string]any
	var protocol string
	switch strings.ToLower(u.Scheme) {
	case "vless":
		outbound, protocol, err = vless(u)
	case "vmess":
		outbound, protocol, err = vmess(u)
	case "trojan":
		outbound, protocol, err = trojan(u)
	case "ss":
		outbound, protocol, err = shadowsocks(u)
	default:
		err = fmt.Errorf("unsupported protocol %q", u.Scheme)
	}
	if err != nil {
		return nil, Metadata{}, err
	}
	outbound["tag"] = "xrayprobe-target"
	applyInterface(outbound, networkInterface)
	config := baseConfig(port, outbound)
	b, err := json.MarshalIndent(config, "", "  ")
	return b, metadataFrom(outbound, protocol), err
}

func buildJSON(input map[string]any, port int, requested, networkInterface string) ([]byte, Metadata, error) {
	if input == nil {
		return nil, Metadata{}, errors.New("JSON config is empty")
	}
	outs, ok := input["outbounds"].([]any)
	if !ok || len(outs) == 0 {
		return nil, Metadata{}, errors.New("JSON config has no outbounds")
	}
	var selected map[string]any
	for _, raw := range outs {
		out, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := out["tag"].(string)
		if requested != "" && tag == requested {
			selected = out
			break
		}
		if requested == "" && eligible(out) && selected == nil {
			selected = out
		}
	}
	if selected == nil {
		if requested != "" {
			return nil, Metadata{}, fmt.Errorf("outbound tag %q was not found or is not eligible", requested)
		}
		return nil, Metadata{}, errors.New("no eligible proxy outbound was found")
	}
	tag, _ := selected["tag"].(string)
	if tag == "" {
		tag = "xrayprobe-target"
		selected["tag"] = tag
	}
	applyInterface(selected, networkInterface)
	inboundTag := fmt.Sprintf("xrayprobe-inbound-%d", port)
	inbounds, _ := input["inbounds"].([]any)
	inbounds = append(inbounds, map[string]any{
		"tag": inboundTag, "listen": "127.0.0.1", "port": port, "protocol": "socks",
		"settings": map[string]any{"auth": "noauth", "udp": false},
	})
	input["inbounds"] = inbounds
	routing, _ := input["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
		input["routing"] = routing
	}
	rules, _ := routing["rules"].([]any)
	rules = append(rules, map[string]any{"type": "field", "inboundTag": []any{inboundTag}, "outboundTag": tag})
	routing["rules"] = rules
	b, err := json.MarshalIndent(input, "", "  ")
	protocol, _ := selected["protocol"].(string)
	return b, metadataFrom(selected, protocol), err
}

func applyInterface(outbound map[string]any, networkInterface string) {
	networkInterface = strings.TrimSpace(networkInterface)
	if networkInterface == "" {
		return
	}
	streamSettings, _ := outbound["streamSettings"].(map[string]any)
	if streamSettings == nil {
		streamSettings = map[string]any{}
		outbound["streamSettings"] = streamSettings
	}
	sockopt, _ := streamSettings["sockopt"].(map[string]any)
	if sockopt == nil {
		sockopt = map[string]any{}
		streamSettings["sockopt"] = sockopt
	}
	sockopt["interface"] = networkInterface
}

func baseConfig(port int, outbound map[string]any) map[string]any {
	return map[string]any{
		"inbounds": []any{map[string]any{
			"tag": "xrayprobe-inbound", "listen": "127.0.0.1", "port": port,
			"protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false},
		}},
		"outbounds": []any{outbound},
	}
}

func vless(u *url.URL) (map[string]any, string, error) {
	if u.User == nil {
		return nil, "", errors.New("VLESS link has no user ID")
	}
	port, err := portOf(u)
	if err != nil {
		return nil, "", err
	}
	user := u.User.Username()
	settings := map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{map[string]any{"id": user, "encryption": queryOr(u, "encryption", "none"), "flow": u.Query().Get("flow")}}}}}
	out := map[string]any{"protocol": "vless", "settings": settings}
	stream := streamSettings(u)
	if len(stream) > 0 {
		out["streamSettings"] = stream
	}
	return out, "vless", nil
}

func vmess(u *url.URL) (map[string]any, string, error) {
	b, err := decodeURLPayload(u, "VMess")
	if err != nil {
		return nil, "", err
	}
	var item struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		AID  any    `json:"aid"`
		Scy  string `json:"scy"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
		ALPN string `json:"alpn"`
		FP   string `json:"fp"`
		PBK  string `json:"pbk"`
		SID  string `json:"sid"`
		SPX  string `json:"spx"`
	}
	if err := json.Unmarshal(b, &item); err != nil {
		return nil, "", fmt.Errorf("decode VMess payload: %w", err)
	}
	if item.Add == "" || item.ID == "" {
		return nil, "", errors.New("VMess link is missing address or UUID")
	}
	port, err := number(item.Port, 443)
	if err != nil {
		return nil, "", fmt.Errorf("VMess port: %w", err)
	}
	aid, _ := number(item.AID, 0)
	user := map[string]any{"id": item.ID, "alterId": aid, "security": queryDefault(item.Scy, "auto")}
	out := map[string]any{"protocol": "vmess", "settings": map[string]any{"vnext": []any{map[string]any{"address": item.Add, "port": port, "users": []any{user}}}}}
	q := url.Values{}
	q.Set("type", queryDefault(item.Net, "tcp"))
	q.Set("host", item.Host)
	q.Set("path", item.Path)
	q.Set("security", item.TLS)
	q.Set("sni", item.SNI)
	q.Set("alpn", item.ALPN)
	q.Set("fp", item.FP)
	q.Set("pbk", item.PBK)
	q.Set("sid", item.SID)
	q.Set("spx", item.SPX)
	stream := streamSettings(&url.URL{Host: item.Add, RawQuery: q.Encode()})
	if len(stream) > 0 {
		out["streamSettings"] = stream
	}
	return out, "vmess", nil
}

func trojan(u *url.URL) (map[string]any, string, error) {
	if u.User == nil {
		return nil, "", errors.New("Trojan link has no password")
	}
	port, err := portOf(u)
	if err != nil {
		return nil, "", err
	}
	out := map[string]any{"protocol": "trojan", "settings": map[string]any{"servers": []any{map[string]any{"address": u.Hostname(), "port": port, "password": u.User.Username()}}}}
	stream := streamSettings(u)
	if len(stream) > 0 {
		out["streamSettings"] = stream
	}
	return out, "trojan", nil
}

func shadowsocks(u *url.URL) (map[string]any, string, error) {
	method, password, host, port, err := parseSS(u)
	if err != nil {
		return nil, "", err
	}
	out := map[string]any{"protocol": "shadowsocks", "settings": map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "method": method, "password": password}}}}
	return out, "shadowsocks", nil
}

func parseSS(u *url.URL) (string, string, string, int, error) {
	if u.User != nil {
		method := u.User.Username()
		password, hasPassword := u.User.Password()
		if !hasPassword {
			if decoded := b64Decode(method); len(decoded) > 0 {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					method, password = parts[0], parts[1]
				}
			}
		}
		port, err := portOf(u)
		return method, password, u.Hostname(), port, err
	}
	b, err := decodeURLPayload(u, "Shadowsocks")
	if err != nil {
		return "", "", "", 0, err
	}
	parts := strings.SplitN(string(b), "@", 2)
	if len(parts) != 2 {
		return "", "", "", 0, errors.New("invalid Shadowsocks payload")
	}
	credentials, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		credentials = b64Decode(parts[0])
	}
	if len(credentials) == 0 {
		return "", "", "", 0, errors.New("invalid Shadowsocks credentials")
	}
	cred := strings.SplitN(string(credentials), ":", 2)
	if len(cred) != 2 {
		return "", "", "", 0, errors.New("invalid Shadowsocks method/password")
	}
	server, err := url.Parse("ss://" + parts[1])
	if err != nil {
		return "", "", "", 0, err
	}
	port, err := portOf(server)
	return cred[0], cred[1], server.Hostname(), port, err
}

func streamSettings(u *url.URL) map[string]any {
	q := u.Query()
	network := queryOr(u, "type", "tcp")
	stream := map[string]any{"network": network}
	security := q.Get("security")
	if security == "" && q.Get("tls") != "" {
		security = "tls"
	}
	if security != "" && security != "none" {
		stream["security"] = security
	}
	serverName := firstNonEmpty(q.Get("sni"), q.Get("serverName"), u.Hostname())
	if security == "tls" {
		settings := map[string]any{"serverName": serverName}
		if fp := q.Get("fp"); fp != "" {
			settings["fingerprint"] = fp
		}
		if alpn := q.Get("alpn"); alpn != "" {
			settings["alpn"] = strings.Split(alpn, ",")
		}
		if q.Get("allowInsecure") == "1" || strings.EqualFold(q.Get("allowInsecure"), "true") {
			settings["allowInsecure"] = true
		}
		stream["tlsSettings"] = settings
	}
	if security == "reality" {
		settings := map[string]any{"serverName": serverName, "fingerprint": queryOr(u, "fp", "chrome")}
		if v := q.Get("pbk"); v != "" {
			settings["publicKey"] = v
		}
		if v := q.Get("sid"); v != "" {
			settings["shortId"] = v
		}
		if v := q.Get("spx"); v != "" {
			settings["spiderX"] = v
		}
		stream["realitySettings"] = settings
	}
	switch network {
	case "ws":
		ws := map[string]any{"path": queryOr(u, "path", "/")}
		if host := q.Get("host"); host != "" {
			ws["headers"] = map[string]any{"Host": host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		stream["grpcSettings"] = map[string]any{"serviceName": q.Get("serviceName"), "multiMode": q.Get("mode") == "multi"}
	case "httpupgrade":
		stream["httpupgradeSettings"] = map[string]any{"path": queryOr(u, "path", "/"), "host": q.Get("host")}
	case "xhttp":
		stream["xhttpSettings"] = map[string]any{"path": queryOr(u, "path", "/"), "mode": queryOr(u, "mode", "auto"), "host": q.Get("host")}
	}
	return stream
}

func eligible(out map[string]any) bool {
	p, _ := out["protocol"].(string)
	switch strings.ToLower(p) {
	case "", "freedom", "blackhole", "dns", "loopback":
		return false
	default:
		return true
	}
}

func metadataFrom(out map[string]any, protocol string) Metadata {
	m := Metadata{Protocol: protocol}
	stream, _ := out["streamSettings"].(map[string]any)
	if stream == nil {
		return m
	}
	m.Transport, _ = stream["network"].(string)
	m.Security, _ = stream["security"].(string)
	return m
}

func portOf(u *url.URL) (int, error) {
	p := u.Port()
	if p == "" {
		return 443, nil
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return 0, errors.New("invalid port")
	}
	return n, nil
}

func decodeURLPayload(u *url.URL, name string) ([]byte, error) {
	payload := strings.TrimPrefix(u.Opaque, "//")
	if payload == "" {
		payload = strings.TrimPrefix(u.Path, "/")
	}
	if payload == "" {
		payload = u.Host
	}
	b := b64Decode(payload)
	if len(b) == 0 {
		return nil, fmt.Errorf("invalid %s payload", name)
	}
	return b, nil
}

func b64Decode(value string) []byte {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", "+")
	value = strings.ReplaceAll(value, "_", "/")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(value); err == nil {
			return b
		}
	}
	return nil
}

func number(value any, fallback int) (int, error) {
	switch v := value.(type) {
	case float64:
		return int(v), nil
	case string:
		if v == "" {
			return fallback, nil
		}
		return strconv.Atoi(v)
	case nil:
		return fallback, nil
	default:
		return 0, errors.New("must be a number")
	}
}

func queryOr(u *url.URL, key, fallback string) string {
	if v := u.Query().Get(key); v != "" {
		return v
	}
	return fallback
}
func queryDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
