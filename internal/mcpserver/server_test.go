package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KiaTheRandomGuy/XrayProbe/internal/core"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/input"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/remote"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/types"
)

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, source input.Source, _ types.RunOptions) ([]types.Result, string, error) {
	if source.Kind == "uri" {
		return []types.Result{{Index: 0, ConfigID: "cfg_one", Name: "one", Core: "v26.3.27", Status: "ok", Score: 88}}, "v26.3.27", nil
	}
	return []types.Result{
		{Index: 0, ConfigID: "cfg_slow", Name: "slow", Core: "v26.3.27", Status: "ok", Score: 70},
		{Index: 1, ConfigID: "cfg_best", Name: "best", Core: "v26.3.27", Status: "ok", Score: 95},
		{Index: 2, ConfigID: "cfg_bad", Name: "bad", Core: "v26.3.27", Status: "failed", Error: "probe failed"},
	}, "v26.3.27", nil
}

type fakeTargetRunner struct {
	source  input.Source
	options types.RunOptions
}

func (f *fakeTargetRunner) Run(_ context.Context, _ string, source input.Source, options types.RunOptions) ([]types.Result, string, error) {
	f.source = source
	f.options = options
	return []types.Result{{Index: 0, ConfigID: "cfg_remote", Name: "remote", Core: "v26.3.27", Status: "ok", Score: 91}}, "v26.3.27", nil
}

func connectTestServer(t *testing.T) (*sdk.ClientSession, func()) {
	t.Helper()
	server, err := New(Options{Manager: &core.Manager{Cache: t.TempDir()}, Service: fakeRunner{}, RequestTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.mcp.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		serverSession.Close()
		t.Fatal(err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}

func TestServerExposesExpectedTools(t *testing.T) {
	session, cleanup := connectTestServer(t)
	defer cleanup()

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"get_xrayprobe_status", "test_xray_config", "test_xray_subscription"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tools = %#v, want %#v", names, want)
	}
}

func TestServerReturnsStructuredStatus(t *testing.T) {
	session, cleanup := connectTestServer(t)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_xrayprobe_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("status tool returned an error: %#v", result.Content)
	}
	var status StatusOutput
	decodeStructured(t, result.StructuredContent, &status)
	if status.XrayProbe != "0.3.0" || status.CurrentCore != "latest" {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestServerReturnsConfigAndRankedSubscription(t *testing.T) {
	session, cleanup := connectTestServer(t)
	defer cleanup()

	configResult, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "test_xray_config",
		Arguments: map[string]any{"source": "vless://one@example.com:443", "source_type": "uri"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config ConfigOutput
	decodeStructured(t, configResult.StructuredContent, &config)
	if config.Result.ConfigID != "cfg_one" || config.Result.Status != "ok" {
		t.Fatalf("unexpected config result: %#v", config)
	}

	batchResult, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name: "test_xray_subscription",
		Arguments: map[string]any{
			"source":           "subscription text",
			"source_type":      "text",
			"result_limit":     1,
			"include_failures": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var batch SubscriptionOutput
	decodeStructured(t, batchResult.StructuredContent, &batch)
	if batch.Best == nil || batch.Best.ConfigID != "cfg_best" || len(batch.Results) != 1 || batch.Results[0].ConfigID != "cfg_best" {
		t.Fatalf("unexpected ranked results: %#v", batch)
	}
	if batch.Summary.Total != 3 || batch.Summary.Succeeded != 2 || batch.Summary.Failed != 1 || len(batch.Failures) != 1 || !batch.Truncated {
		t.Fatalf("unexpected batch summary: %#v", batch)
	}
}

func TestServerMaterializesLocalFileForRemoteTarget(t *testing.T) {
	root := t.TempDir()
	path := root + "/config.txt"
	if err := os.WriteFile(path, []byte("vless://remote@example.com:443"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeRemote := &fakeTargetRunner{}
	server, err := New(Options{
		Manager:       &core.Manager{Cache: t.TempDir()},
		Service:       fakeRunner{},
		Remote:        fakeRemote,
		RemoteTargets: []remote.Target{{Name: "probe", Address: "root@example.com"}},
		AllowedRoots:  []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, coreVersion, target, err := server.run(context.Background(), "probe", input.Source{Value: path, Kind: "path"}, types.RunOptions{RestrictPaths: true})
	if err != nil {
		t.Fatal(err)
	}
	if target != "probe" || coreVersion != "v26.3.27" || len(results) != 1 || results[0].ConfigID != "cfg_remote" {
		t.Fatalf("unexpected remote result: target=%q core=%q results=%#v", target, coreVersion, results)
	}
	if fakeRemote.source.Kind != "text" || fakeRemote.source.Value != "vless://remote@example.com:443" || fakeRemote.options.RestrictPaths {
		t.Fatalf("remote source/options were not materialized safely: source=%#v options=%#v", fakeRemote.source, fakeRemote.options)
	}
}

func decodeStructured(t *testing.T, value any, target any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		t.Fatalf("decode structured content: %v (%s)", err, b)
	}
}
