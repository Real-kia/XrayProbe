package types

import "time"

type Spec struct {
	Index    int
	Name     string
	Source   string
	Kind     string
	URI      string
	Config   map[string]any
	Protocol string
}

type ProbeOptions struct {
	Attempts      int
	Timeout       time.Duration
	ProbeURL      string
	MetadataURL   string
	NoMetadata    bool
	Speed         bool
	DownloadBytes int64
	// OnAttempt, if set, is called synchronously after each probe attempt.
	// CLI-only: never set by the MCP server or remote worker, and excluded
	// from JSON so a remote WorkerRequest can still be marshaled.
	OnAttempt func(AttemptEvent) `json:"-"`
}

// AttemptEvent reports the outcome of a single probe attempt as it happens,
// so a caller can show progress instead of waiting silently for the batch
// of attempts to finish.
type AttemptEvent struct {
	Index     int
	Total     int
	OK        bool
	LatencyMS float64
	Reason    string
}

type Result struct {
	Index      int           `json:"index"`
	ConfigID   string        `json:"config_id"`
	Name       string        `json:"name"`
	Protocol   string        `json:"protocol,omitempty"`
	Transport  string        `json:"transport,omitempty"`
	Security   string        `json:"security,omitempty"`
	Core       string        `json:"core_version,omitempty"`
	Status     string        `json:"status"`
	Outbound   *OutboundInfo `json:"outbound,omitempty"`
	Metrics    *Metrics      `json:"metrics,omitempty"`
	Score      float64       `json:"score,omitempty"`
	Grade      string        `json:"grade,omitempty"`
	Error      string        `json:"error,omitempty"`
	DurationMS int64         `json:"duration_ms,omitempty"`
}

type OutboundInfo struct {
	IP          string `json:"ip,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	City        string `json:"city,omitempty"`
	ASN         string `json:"asn,omitempty"`
	Org         string `json:"org,omitempty"`
}

type Metrics struct {
	Attempts        int     `json:"attempts"`
	Successful      int     `json:"successful"`
	SuccessRate     float64 `json:"success_rate"`
	MinLatencyMS    float64 `json:"min_latency_ms,omitempty"`
	MedianLatencyMS float64 `json:"median_latency_ms,omitempty"`
	P95LatencyMS    float64 `json:"p95_latency_ms,omitempty"`
	JitterMS        float64 `json:"jitter_ms,omitempty"`
	DownloadMbps    float64 `json:"download_mbps,omitempty"`
}

type RunOptions struct {
	SourceKind    string
	AllowedRoots  []string
	RestrictPaths bool
	CoreVersion   string
	OutboundTag   string
	Interface     string
	Concurrency   int
	MaxConfigs    int
	Probe         ProbeOptions
	// Progress, if set, is called as each config starts and finishes
	// testing. CLI-only: never set by the MCP server or remote worker, and
	// excluded from JSON so a remote WorkerRequest can still be marshaled.
	Progress func(ProgressEvent) `json:"-"`
}

// ProgressEvent reports that one config's test has started or finished, so
// a caller can show that something is happening instead of waiting silently
// for a whole batch (which can take minutes for a large subscription).
type ProgressEvent struct {
	Index  int
	Total  int
	Name   string
	Phase  string // "start" or "done"
	Status string // set on "done": "ok" or "failed"
	Score  float64
	Grade  string
	Error  string
}
