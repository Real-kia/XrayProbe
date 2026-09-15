package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Real-kia/XrayProbe/internal/types"
)

func Run(ctx context.Context, address string, options types.ProbeOptions) (*types.OutboundInfo, *types.Metrics, error) {
	if options.Attempts <= 0 {
		options.Attempts = 5
	}
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Second
	}
	if options.ProbeURL == "" {
		options.ProbeURL = "https://www.cloudflare.com/cdn-cgi/trace"
	}
	client := clientFor(address, options.Timeout)
	var outbound *types.OutboundInfo
	if !options.NoMetadata {
		metadataURL := options.MetadataURL
		if metadataURL == "" {
			metadataURL = "https://ipwho.is/"
		}
		var err error
		outbound, err = metadata(ctx, client, metadataURL)
		if err != nil {
			outbound = &types.OutboundInfo{}
		}
	}

	latencies := make([]float64, 0, options.Attempts)
	for i := 0; i < options.Attempts; i++ {
		attemptCtx, cancel := context.WithTimeout(ctx, options.Timeout)
		start := time.Now()
		err := request(client, attemptCtx, options.ProbeURL)
		elapsed := float64(time.Since(start).Microseconds()) / 1000
		cancel()
		if err == nil {
			latencies = append(latencies, elapsed)
		}
	}
	metrics := summarize(options.Attempts, latencies)
	if options.Speed && len(latencies) > 0 {
		bytes := options.DownloadBytes
		if bytes <= 0 {
			bytes = 10 << 20
		}
		metrics.DownloadMbps = download(ctx, client, "https://speed.cloudflare.com/__down?bytes="+strconv.FormatInt(bytes, 10), options.Timeout, bytes)
	}
	if len(latencies) == 0 {
		return outbound, metrics, errors.New("all probe requests failed")
	}
	return outbound, metrics, nil
}

func clientFor(address string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
			return dialSOCKS5(ctx, address, target)
		},
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: timeout,
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func request(client *http.Client, ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return errors.New("probe URL must be HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "xrayprobe/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("probe returned %s", resp.Status)
	}
	return nil
}

func metadata(ctx context.Context, client *http.Client, rawURL string) (*types.OutboundInfo, error) {
	if !strings.HasSuffix(rawURL, "/") && !strings.Contains(rawURL, "?") {
		rawURL += "/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xrayprobe/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("metadata returned %s", resp.Status)
	}
	var value struct {
		Success     bool   `json:"success"`
		IP          string `json:"ip"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		City        string `json:"city"`
		Connection  struct {
			ASN any    `json:"asn"`
			Org string `json:"org"`
		} `json:"connection"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&value); err != nil {
		return nil, err
	}
	if !value.Success && value.IP == "" {
		return nil, errors.New("metadata lookup failed")
	}
	return &types.OutboundInfo{IP: value.IP, Country: value.Country, CountryCode: value.CountryCode, City: value.City, ASN: fmt.Sprint(value.Connection.ASN), Org: value.Connection.Org}, nil
}

func summarize(attempts int, values []float64) *types.Metrics {
	m := &types.Metrics{Attempts: attempts, Successful: len(values)}
	if attempts > 0 {
		m.SuccessRate = float64(len(values)) * 100 / float64(attempts)
	}
	if len(values) == 0 {
		return m
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	m.MinLatencyMS = sorted[0]
	m.MedianLatencyMS = percentile(sorted, 0.50)
	m.P95LatencyMS = percentile(sorted, 0.95)
	if len(values) > 1 {
		for i := 1; i < len(values); i++ {
			m.JitterMS += abs(values[i] - values[i-1])
		}
		m.JitterMS /= float64(len(values) - 1)
	}
	return m
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	position := float64(len(values)-1) * fraction
	low := int(math.Floor(position))
	high := int(math.Ceil(position))
	if high >= len(values) {
		high = len(values) - 1
	}
	if low == high {
		return values[low]
	}
	weight := position - float64(low)
	return values[low] + (values[high]-values[low])*weight
}

func Score(m *types.Metrics) float64 {
	if m == nil || m.Attempts == 0 {
		return 0
	}
	latency := 100 - (m.MedianLatencyMS / 1000 * 100)
	if latency < 0 {
		latency = 0
	}
	jitter := 100 - (m.JitterMS / 250 * 100)
	if jitter < 0 {
		jitter = 0
	}
	score := m.SuccessRate*0.50 + latency*0.35 + jitter*0.15
	if score > 100 {
		return 100
	}
	if score < 0 {
		return 0
	}
	return score
}

func Grade(score float64) string {
	switch {
	case score >= 85:
		return "Excellent"
	case score >= 70:
		return "Good"
	case score >= 50:
		return "Fair"
	default:
		return "Poor"
	}
}

func download(ctx context.Context, client *http.Client, rawURL string, timeout time.Duration, expected int64) float64 {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, expected))
	if err != nil || n == 0 {
		return 0
	}
	seconds := time.Since(start).Seconds()
	if seconds <= 0 {
		return 0
	}
	return float64(n*8) / seconds / 1_000_000
}

func dialSOCKS5(ctx context.Context, proxyAddr, target string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	closeOnError := func(e error) (net.Conn, error) { conn.Close(); return nil, e }
	if _, err = conn.Write([]byte{5, 1, 0}); err != nil {
		return closeOnError(err)
	}
	method := make([]byte, 2)
	if _, err = io.ReadFull(conn, method); err != nil {
		return closeOnError(err)
	}
	if method[1] != 0 {
		return closeOnError(errors.New("Xray SOCKS proxy requires unsupported authentication"))
	}
	host, portString, err := net.SplitHostPort(target)
	if err != nil {
		return closeOnError(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		return closeOnError(err)
	}
	request := []byte{5, 1, 0}
	ip := net.ParseIP(host)
	if ip4 := ip.To4(); ip4 != nil {
		request = append(request, 1)
		request = append(request, ip4...)
	} else if ip6 := ip.To16(); ip6 != nil {
		request = append(request, 4)
		request = append(request, ip6...)
	} else {
		if len(host) > 255 {
			return closeOnError(errors.New("target hostname is too long"))
		}
		request = append(request, 3, byte(len(host)))
		request = append(request, host...)
	}
	request = append(request, byte(port>>8), byte(port))
	if _, err = conn.Write(request); err != nil {
		return closeOnError(err)
	}
	response := make([]byte, 4)
	if _, err = io.ReadFull(conn, response); err != nil {
		return closeOnError(err)
	}
	if response[1] != 0 {
		return closeOnError(fmt.Errorf("SOCKS proxy connection failed with code %d", response[1]))
	}
	var length int
	switch response[3] {
	case 1:
		length = 4
	case 4:
		length = 16
	case 3:
		one := []byte{0}
		if _, err = io.ReadFull(conn, one); err != nil {
			return closeOnError(err)
		}
		length = int(one[0])
	default:
		return closeOnError(errors.New("invalid SOCKS response"))
	}
	if _, err = io.CopyN(io.Discard, bufio.NewReader(conn), int64(length+2)); err != nil {
		return closeOnError(err)
	}
	return conn, nil
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
