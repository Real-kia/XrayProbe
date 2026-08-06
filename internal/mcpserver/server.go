package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KiaTheRandomGuy/XrayProbe/internal/core"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/input"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/remote"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/tester"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/types"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/version"
)

type Options struct {
	Manager        *core.Manager
	Service        Runner
	Remote         TargetRunner
	RemoteTargets  []remote.Target
	AllowedRoots   []string
	MaxConcurrent  int
	RequestTimeout time.Duration
}

type Runner interface {
	Run(context.Context, input.Source, types.RunOptions) ([]types.Result, string, error)
}

type TargetRunner interface {
	Run(context.Context, string, input.Source, types.RunOptions) ([]types.Result, string, error)
}

type Server struct {
	mcp            *sdk.Server
	manager        *core.Manager
	service        Runner
	remote         TargetRunner
	remoteTargets  []string
	allowedRoots   []string
	maxConcurrent  int
	requestTimeout time.Duration
	semaphore      chan struct{}
}

type ConfigInput struct {
	Source          string `json:"source" jsonschema:"share link, JSON document, HTTPS subscription URL, subscription text, or an allowed file path"`
	SourceType      string `json:"source_type,omitempty" jsonschema:"auto, uri, json, path, url, or text; defaults to auto"`
	CoreVersion     string `json:"core_version,omitempty" jsonschema:"optional Xray-core version such as v26.3.27; latest is used by default"`
	OutboundTag     string `json:"outbound_tag,omitempty" jsonschema:"optional outbound tag for a JSON config with multiple outbounds"`
	Attempts        int    `json:"attempts,omitempty" jsonschema:"number of HTTPS probe attempts from 1 to 20; default 5"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty" jsonschema:"timeout for each probe request from 1 to 120 seconds; default 10"`
	IncludeMetadata *bool  `json:"include_metadata,omitempty" jsonschema:"whether to look up outbound IP, country, and ASN; default true"`
	ProbeURL        string `json:"probe_url,omitempty" jsonschema:"optional custom HTTPS health endpoint"`
	MetadataURL     string `json:"metadata_url,omitempty" jsonschema:"optional custom HTTPS IP metadata endpoint"`
	Speed           bool   `json:"speed,omitempty" jsonschema:"run an opt-in download speed sample"`
	DownloadBytes   int64  `json:"download_bytes,omitempty" jsonschema:"bytes for the optional speed sample; default 10485760"`
	Target          string `json:"target,omitempty" jsonschema:"local or a configured remote SSH target; defaults to local"`
}

type SubscriptionInput struct {
	ConfigInput
	Concurrency     int   `json:"concurrency,omitempty" jsonschema:"parallel config tests from 1 to 16; default 4"`
	MaxConfigs      int   `json:"max_configs,omitempty" jsonschema:"maximum configs to decode and test from 1 to 500; default 500"`
	ResultLimit     int   `json:"result_limit,omitempty" jsonschema:"number of ranked working results from 1 to 100; default 10"`
	IncludeFailures *bool `json:"include_failures,omitempty" jsonschema:"include compact failed-config entries; default true"`
}

type ConfigOutput struct {
	SchemaVersion int          `json:"schema_version"`
	Target        string       `json:"target"`
	Result        types.Result `json:"result"`
}

type BatchSummary struct {
	Total       int    `json:"total"`
	Succeeded   int    `json:"succeeded"`
	Failed      int    `json:"failed"`
	CoreVersion string `json:"core_version"`
	DurationMS  int64  `json:"duration_ms"`
	Target      string `json:"target"`
}

type Failure struct {
	ConfigID string `json:"config_id"`
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Error    string `json:"error"`
}

type SubscriptionOutput struct {
	SchemaVersion int            `json:"schema_version"`
	Summary       BatchSummary   `json:"summary"`
	Best          *types.Result  `json:"best,omitempty"`
	Results       []types.Result `json:"results"`
	Failures      []Failure      `json:"failures,omitempty"`
	Truncated     bool           `json:"truncated"`
}

type StatusOutput struct {
	SchemaVersion  int      `json:"schema_version"`
	XrayProbe      string   `json:"xrayprobe_version"`
	Platform       string   `json:"platform"`
	CurrentCore    string   `json:"current_core"`
	InstalledCores []string `json:"installed_cores"`
	CacheDirectory string   `json:"cache_directory"`
	AllowedRoots   []string `json:"allowed_roots"`
	RemoteTargets  []string `json:"remote_targets"`
	MaxConcurrent  int      `json:"max_concurrent_tests"`
	RequestTimeout string   `json:"request_timeout"`
}

func New(options Options) (*Server, error) {
	if options.Manager == nil {
		return nil, errors.New("MCP server has no core manager")
	}
	if options.Service == nil {
		options.Service = tester.NewService(options.Manager)
	}
	if options.MaxConcurrent <= 0 {
		options.MaxConcurrent = 2
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 10 * time.Minute
	}
	roots, err := input.NormalizeAllowedRoots(options.AllowedRoots)
	if err != nil {
		return nil, err
	}
	remoteRunner := options.Remote
	remoteTargetNames := make([]string, 0, len(options.RemoteTargets))
	if remoteRunner == nil && len(options.RemoteTargets) > 0 {
		remoteRunner, err = remote.New(options.RemoteTargets, options.RequestTimeout)
		if err != nil {
			return nil, err
		}
	}
	for _, target := range options.RemoteTargets {
		remoteTargetNames = append(remoteTargetNames, target.Name)
	}
	sort.Strings(remoteTargetNames)

	s := &Server{
		manager:        options.Manager,
		service:        options.Service,
		remote:         remoteRunner,
		remoteTargets:  remoteTargetNames,
		allowedRoots:   roots,
		maxConcurrent:  options.MaxConcurrent,
		requestTimeout: options.RequestTimeout,
		semaphore:      make(chan struct{}, options.MaxConcurrent),
	}
	s.mcp = sdk.NewServer(&sdk.Implementation{Name: "xrayprobe", Version: version.Value}, nil)
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "test_xray_config",
		Title:       "Test one Xray config",
		Description: "Run one Xray share link or JSON config through Xray-core and return its outbound IP, location, latency, reliability, and quality score. Use target to select a configured remote SSH probe machine; otherwise it runs locally.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: boolPtr(true), DestructiveHint: boolPtr(false)},
	}, s.testConfig)
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "test_xray_subscription",
		Title:       "Rank Xray subscription configs",
		Description: "Decode and test a plain-text or Base64 Xray subscription, then return a compact summary, the best result, ranked working configs, and optional failures. Use target to run the batch from a configured remote SSH probe machine.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: boolPtr(true), DestructiveHint: boolPtr(false)},
	}, s.testSubscription)
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "get_xrayprobe_status",
		Title:       "Get XrayProbe status",
		Description: "Return the XrayProbe version, current and installed local Xray-core versions, file roots, configured remote targets, and MCP limits. This tool does not contact remote services.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPtr(false), DestructiveHint: boolPtr(false)},
	}, s.status)
	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &sdk.StdioTransport{})
}

func (s *Server) testConfig(ctx context.Context, _ *sdk.CallToolRequest, in ConfigInput) (*sdk.CallToolResult, ConfigOutput, error) {
	var output ConfigOutput
	err := s.withSlot(ctx, func(ctx context.Context) error {
		options, err := s.runOptions(in, 1, 1)
		if err != nil {
			return err
		}
		results, _, target, err := s.run(ctx, in.Target, input.Source{Value: in.Source, Kind: sourceKind(in.SourceType, "auto")}, options)
		if err != nil {
			return safeError(err, in.Source)
		}
		if len(results) != 1 {
			return errors.New("test_xray_config expected exactly one config; use test_xray_subscription for batches")
		}
		output = ConfigOutput{SchemaVersion: 1, Target: target, Result: results[0]}
		return nil
	})
	return nil, output, err
}

func (s *Server) testSubscription(ctx context.Context, _ *sdk.CallToolRequest, in SubscriptionInput) (*sdk.CallToolResult, SubscriptionOutput, error) {
	var output SubscriptionOutput
	err := s.withSlot(ctx, func(ctx context.Context) error {
		start := time.Now()
		options, err := s.runOptions(in.ConfigInput, in.Concurrency, in.MaxConfigs)
		if err != nil {
			return err
		}
		results, coreVersion, target, err := s.run(ctx, in.Target, input.Source{Value: in.Source, Kind: sourceKind(in.SourceType, "auto")}, options)
		if err != nil {
			return safeError(err, in.Source)
		}
		limit := in.ResultLimit
		if limit <= 0 {
			limit = 10
		}
		if limit > 100 {
			return errors.New("result_limit must be between 1 and 100")
		}
		includeFailures := true
		if in.IncludeFailures != nil {
			includeFailures = *in.IncludeFailures
		}
		sort.SliceStable(results, func(i, j int) bool {
			if results[i].Status != results[j].Status {
				return results[i].Status == "ok"
			}
			return results[i].Score > results[j].Score
		})
		summary := BatchSummary{Total: len(results), CoreVersion: coreVersion, DurationMS: time.Since(start).Milliseconds(), Target: target}
		for _, result := range results {
			if result.Status == "ok" {
				summary.Succeeded++
			} else {
				summary.Failed++
			}
		}
		for _, result := range results {
			if result.Status == "ok" {
				copy := result
				output.Best = &copy
				break
			}
		}
		for _, result := range results {
			if result.Status == "ok" && len(output.Results) < limit {
				output.Results = append(output.Results, result)
			}
		}
		if summary.Succeeded > len(output.Results) {
			output.Truncated = true
		}
		if includeFailures {
			for _, result := range results {
				if result.Status != "ok" {
					output.Failures = append(output.Failures, Failure{ConfigID: result.ConfigID, Index: result.Index, Name: result.Name, Error: result.Error})
				}
			}
		}
		output.SchemaVersion = 1
		output.Summary = summary
		return nil
	})
	return nil, output, err
}

func (s *Server) status(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, StatusOutput, error) {
	current, err := s.manager.Current()
	if err != nil {
		return nil, StatusOutput{}, err
	}
	installed, err := s.manager.Installed()
	if err != nil {
		return nil, StatusOutput{}, err
	}
	select {
	case <-ctx.Done():
		return nil, StatusOutput{}, ctx.Err()
	default:
	}
	return nil, StatusOutput{SchemaVersion: 1, XrayProbe: version.Value, Platform: runtime.GOOS + "/" + runtime.GOARCH, CurrentCore: current, InstalledCores: installed, CacheDirectory: s.manager.Cache, AllowedRoots: append([]string(nil), s.allowedRoots...), RemoteTargets: append([]string(nil), s.remoteTargets...), MaxConcurrent: s.maxConcurrent, RequestTimeout: s.requestTimeout.String()}, nil
}

func (s *Server) run(ctx context.Context, target string, source input.Source, options types.RunOptions) ([]types.Result, string, string, error) {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "local") {
		results, coreVersion, err := s.service.Run(ctx, source, options)
		return results, coreVersion, "local", err
	}
	if s.remote == nil {
		return nil, "", "", fmt.Errorf("remote target %q is not configured; start the MCP server with --target NAME=SSH_ADDRESS", target)
	}
	remoteSource, err := s.prepareRemoteSource(source)
	if err != nil {
		return nil, "", target, err
	}
	options.AllowedRoots = nil
	options.RestrictPaths = false
	results, coreVersion, err := s.remote.Run(ctx, target, remoteSource, options)
	return results, coreVersion, target, err
}

func (s *Server) prepareRemoteSource(source input.Source) (input.Source, error) {
	kind := sourceKind(source.Kind, "auto")
	if kind == "path" {
		return s.readLocalSource(source.Value)
	}
	if kind != "auto" {
		return source, nil
	}
	trimmed := strings.TrimSpace(source.Value)
	if strings.HasPrefix(trimmed, "{") || strings.ContainsAny(trimmed, "\r\n") {
		return input.Source{Value: source.Value, Kind: "text"}, nil
	}
	if strings.Contains(trimmed, "://") {
		return source, nil
	}
	return s.readLocalSource(source.Value)
}

func (s *Server) readLocalSource(path string) (input.Source, error) {
	content, err := input.ReadFile(path, input.LoadOptions{AllowedRoots: s.allowedRoots, RestrictPaths: true})
	if err != nil {
		return input.Source{}, fmt.Errorf("read local source for remote target: %w", err)
	}
	return input.Source{Value: string(content), Kind: "text"}, nil
}

func (s *Server) runOptions(in ConfigInput, concurrency, maxConfigs int) (types.RunOptions, error) {
	if in.Attempts < 0 || in.Attempts > 20 {
		return types.RunOptions{}, errors.New("attempts must be between 1 and 20")
	}
	if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 120 {
		return types.RunOptions{}, errors.New("timeout_seconds must be between 1 and 120")
	}
	if concurrency < 0 || concurrency > 16 {
		return types.RunOptions{}, errors.New("concurrency must be between 1 and 16")
	}
	if maxConfigs < 0 || maxConfigs > 500 {
		return types.RunOptions{}, errors.New("max_configs must be between 1 and 500")
	}
	attempts := in.Attempts
	if attempts == 0 {
		attempts = 5
	}
	timeout := in.TimeoutSeconds
	if timeout == 0 {
		timeout = 10
	}
	if concurrency == 0 {
		concurrency = 4
	}
	if maxConfigs == 0 {
		maxConfigs = 500
	}
	includeMetadata := true
	if in.IncludeMetadata != nil {
		includeMetadata = *in.IncludeMetadata
	}
	bytes := in.DownloadBytes
	if bytes == 0 {
		bytes = 10 << 20
	}
	if bytes < 1<<20 || bytes > 100<<20 {
		return types.RunOptions{}, errors.New("download_bytes must be between 1048576 and 104857600")
	}
	return types.RunOptions{
		SourceKind: in.SourceType, AllowedRoots: s.allowedRoots, RestrictPaths: true,
		CoreVersion: in.CoreVersion, OutboundTag: in.OutboundTag, Concurrency: concurrency, MaxConfigs: maxConfigs,
		Probe: types.ProbeOptions{Attempts: attempts, Timeout: time.Duration(timeout) * time.Second, ProbeURL: in.ProbeURL, MetadataURL: in.MetadataURL, NoMetadata: !includeMetadata, Speed: in.Speed, DownloadBytes: bytes},
	}, nil
}

func (s *Server) withSlot(ctx context.Context, fn func(context.Context) error) error {
	requestCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-requestCtx.Done():
		return requestCtx.Err()
	}
	return fn(requestCtx)
}

func sourceKind(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func safeError(err error, source string) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), source, "[redacted source]")
	return errors.New(message)
}

func boolPtr(value bool) *bool { return &value }
