package probe

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Real-kia/XrayProbe/internal/types"
)

// GeminiURL is the page Google challenges when the exit IP is flagged.
const GeminiURL = "https://gemini.google.com/app"

const geminiUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

const geminiMaxRedirects = 5

// Gemini loads the Gemini web app through the SOCKS proxy at address,
// following redirects by hand so the verdict mirrors what a browser sees:
// HTTP 200 is "ok", HTTP 429 or any hop through Google's /sorry/ page is
// "blocked", and everything else (including network errors) is "error".
func Gemini(ctx context.Context, address string, timeout time.Duration) *types.GeminiInfo {
	return geminiAt(ctx, address, GeminiURL, timeout)
}

func geminiAt(ctx context.Context, address, start string, timeout time.Duration) *types.GeminiInfo {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := clientFor(address, timeout)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	current := start
	info := &types.GeminiInfo{Status: "error"}
	for hop := 0; hop <= geminiMaxRedirects; hop++ {
		info.FinalURL = current
		if strings.Contains(current, "/sorry/") {
			info.Status = "blocked"
			return info
		}
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, current, nil)
		if err != nil {
			cancel()
			info.Error = err.Error()
			return info
		}
		req.Header.Set("User-Agent", geminiUserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			info.Error = ClassifyError(err)
			return info
		}
		resp.Body.Close()
		cancel()
		info.HTTP = resp.StatusCode
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			info.Status = "blocked"
			return info
		case resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.Header.Get("Location") != "":
			base, _ := url.Parse(current)
			next, err := base.Parse(resp.Header.Get("Location"))
			if err != nil {
				info.Error = err.Error()
				return info
			}
			current = next.String()
			continue
		case resp.StatusCode == http.StatusOK:
			info.Status = "ok"
			return info
		}
		return info
	}
	info.Error = "too many redirects"
	return info
}
