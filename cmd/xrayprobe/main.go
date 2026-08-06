package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/KiaTheRandomGuy/XrayProbe/internal/core"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/mcpserver"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/output"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/remote"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/tester"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/types"
	versioninfo "github.com/KiaTheRandomGuy/XrayProbe/internal/version"
)

const version = versioninfo.Value

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stdout)
		return 0
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(os.Stdout)
		return 0
	}
	if args[0] == "version" {
		fmt.Printf("xrayprobe %s\n", version)
		return 0
	}
	manager, err := core.NewManager()
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 3
	}
	switch args[0] {
	case "test":
		return testCommand(manager, args[1:])
	case "core":
		return coreCommand(manager, args[1:])
	case "mcp":
		return mcpCommand(manager, args[1:])
	case "remote-worker":
		return remoteWorkerCommand(manager, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "xrayprobe: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func mcpCommand(manager *core.Manager, args []string) int {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			mcpUsage(os.Stdout)
			return 0
		}
	}
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var roots stringListFlag
	fs.Var(&roots, "allow-path", "allow MCP file inputs from this directory; repeatable")
	var targets stringListFlag
	fs.Var(&targets, "target", "configure a remote probe target as NAME=SSH_ADDRESS; repeatable")
	maxConcurrent := fs.Int("max-concurrent-tests", 2, "maximum concurrent MCP requests")
	requestTimeout := fs.Duration("request-timeout", 10*time.Minute, "maximum duration of one MCP request")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *maxConcurrent < 1 || *requestTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "usage: xrayprobe mcp [--allow-path DIR] [--target NAME=SSH_ADDRESS] [--max-concurrent-tests N] [--request-timeout D]")
		return 2
	}
	remoteTargets, err := remote.ParseTargets(targets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 2
	}
	server, err := mcpserver.New(mcpserver.Options{Manager: manager, AllowedRoots: roots, RemoteTargets: remoteTargets, MaxConcurrent: *maxConcurrent, RequestTimeout: *requestTimeout})
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 3
	}
	if err := server.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 3
	}
	return 0
}

func remoteWorkerCommand(manager *core.Manager, args []string) int {
	if len(args) != 0 {
		return 2
	}
	var request remote.WorkerRequest
	response := remote.WorkerResponse{}
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		response.Error = fmt.Sprintf("decode remote test request: %v", err)
		_ = json.NewEncoder(os.Stdout).Encode(response)
		return 0
	}
	results, coreVersion, err := tester.NewService(manager).Run(context.Background(), request.Source, request.Options)
	response.Results = results
	response.CoreVersion = coreVersion
	if err != nil {
		response.Error = err.Error()
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		return 3
	}
	return 0
}

func testCommand(manager *core.Manager, args []string) int {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			testUsage(os.Stdout)
			return 0
		}
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	coreVersion := fs.String("core-version", "", "Xray-core version or latest")
	outbound := fs.String("outbound", "", "JSON outbound tag")
	concurrency := fs.Int("concurrency", 4, "maximum concurrent subscription tests")
	maxConfigs := fs.Int("max-configs", 500, "maximum subscription configs")
	attempts := fs.Int("attempts", 5, "probe attempts per config")
	timeout := fs.Duration("timeout", 10*time.Second, "timeout per probe request")
	probeURL := fs.String("probe-url", "", "HTTPS endpoint used for latency probes")
	metadataURL := fs.String("metadata-url", "", "HTTPS IP metadata endpoint")
	noMetadata := fs.Bool("no-metadata", false, "skip outbound IP and location lookup")
	speed := fs.Bool("speed", false, "run an opt-in download speed sample")
	downloadBytes := fs.Int64("download-size", 10<<20, "bytes to request for --speed")
	format := fs.String("format", "table", "table, json, or csv")
	outputPath := fs.String("output", "", "write results to a file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: xrayprobe test [options] <URI|file|subscription-URL>")
		return 2
	}
	if *concurrency < 1 || *attempts < 1 || *maxConfigs < 1 {
		fmt.Fprintln(os.Stderr, "concurrency, attempts, and max-configs must be positive")
		return 2
	}
	runOptions := types.RunOptions{CoreVersion: *coreVersion, OutboundTag: *outbound, Concurrency: *concurrency, MaxConfigs: *maxConfigs, Probe: types.ProbeOptions{Attempts: *attempts, Timeout: *timeout, ProbeURL: *probeURL, MetadataURL: *metadataURL, NoMetadata: *noMetadata, Speed: *speed, DownloadBytes: *downloadBytes}}
	results, _, err := tester.Run(context.Background(), manager, fs.Arg(0), runOptions)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 3
	}
	var writer io.Writer = os.Stdout
	var file *os.File
	if *outputPath != "" {
		file, err = os.OpenFile(*outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xrayprobe:", err)
			return 3
		}
		defer file.Close()
		writer = file
	}
	if err := output.Render(writer, results, *format, true); err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 2
	}
	for _, result := range results {
		if result.Status == "ok" {
			return 0
		}
	}
	return 1
}

func coreCommand(manager *core.Manager, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: xrayprobe core install|use|list|current")
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "install":
		version := "latest"
		if len(args) > 1 {
			version = args[1]
		}
		if len(args) > 2 {
			return 2
		}
		path, resolved, err := manager.Ensure(ctx, version)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xrayprobe:", err)
			return 3
		}
		if err := manager.SetCurrent(resolved); err != nil {
			fmt.Fprintln(os.Stderr, "xrayprobe:", err)
			return 3
		}
		fmt.Printf("installed Xray-core %s\n%s\n", resolved, path)
		return 0
	case "use":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: xrayprobe core use <version>")
			return 2
		}
		_, resolved, err := manager.Ensure(ctx, args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "xrayprobe:", err)
			return 3
		}
		if err := manager.SetCurrent(resolved); err != nil {
			fmt.Fprintln(os.Stderr, "xrayprobe:", err)
			return 3
		}
		fmt.Println("using", resolved)
		return 0
	case "current":
		value, err := manager.Current()
		if err != nil {
			fmt.Fprintln(os.Stderr, "xrayprobe:", err)
			return 3
		}
		fmt.Println(value)
		return 0
	case "list":
		fs := flag.NewFlagSet("core list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		remote := fs.Bool("remote", false, "query GitHub releases")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			return 2
		}
		if *remote {
			releases, err := manager.List(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "xrayprobe:", err)
				return 3
			}
			for _, release := range releases {
				marker := ""
				if !release.Prerelease && !release.Draft {
					marker = " stable"
				}
				fmt.Println(release.TagName + marker)
			}
			return 0
		}
		installed, err := manager.Installed()
		if err != nil {
			fmt.Fprintln(os.Stderr, "xrayprobe:", err)
			return 3
		}
		for _, value := range installed {
			fmt.Println(value)
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown core command", args[0])
		return 2
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `xrayprobe - download Xray-core, test configs, and rank connection quality

Usage:
  xrayprobe test [options] <URI|JSON-file|subscription-URL>
  xrayprobe core install [latest|VERSION]
  xrayprobe core use <VERSION>
  xrayprobe core list [--remote]
  xrayprobe core current
  xrayprobe mcp [options]
  xrayprobe version

Examples:
  xrayprobe test 'vless://...'
  xrayprobe test ./config.json --format json
  xrayprobe test https://example.com/subscription.txt --concurrency 4
  xrayprobe core install v26.3.27

Use "xrayprobe test --help" for test options.
Use "xrayprobe mcp --help" for MCP server options.
`)
}

func testUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  xrayprobe test [options] <URI|JSON-file|subscription-URL>

Options:
  --core-version latest|vX.Y.Z  Select the Xray-core release
  --outbound TAG                Select a JSON outbound explicitly
  --attempts N                  Probe attempts per config (default: 5)
  --timeout 10s                 Timeout for each probe request
  --concurrency N               Subscription workers (default: 4)
  --max-configs N               Subscription config limit (default: 500)
  --format table|json|csv       Output format (default: table)
  --output FILE                 Write output to a file
  --no-metadata                 Skip IP/location/ASN lookup
  --probe-url URL               Custom HTTPS health endpoint
  --metadata-url URL            Custom HTTPS metadata endpoint
  --speed                       Run an opt-in download sample
  --download-size BYTES         Bytes for --speed (default: 10485760)
`)
}

func mcpUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  xrayprobe mcp [options]

Run the local stdio MCP server for AI clients. File inputs are limited to the
current working directory unless one or more --allow-path directories are set.

Options:
  --allow-path DIR             Allow MCP file inputs under DIR (repeatable)
  --target NAME=SSH_ADDRESS    Configure a remote probe target (repeatable)
  --max-concurrent-tests N     Maximum concurrent MCP requests (default: 2)
  --request-timeout D          Maximum duration of one MCP request (default: 10m)
`)
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }

func (f *stringListFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("path cannot be empty")
	}
	*f = append(*f, value)
	return nil
}
