package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Real-kia/XrayProbe/internal/core"
	"github.com/Real-kia/XrayProbe/internal/mcpserver"
	"github.com/Real-kia/XrayProbe/internal/netutil"
	"github.com/Real-kia/XrayProbe/internal/output"
	"github.com/Real-kia/XrayProbe/internal/remote"
	"github.com/Real-kia/XrayProbe/internal/tester"
	"github.com/Real-kia/XrayProbe/internal/types"
	versioninfo "github.com/Real-kia/XrayProbe/internal/version"
)

var version = versioninfo.Value

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
	if len(args) > 0 && args[0] == "setup" {
		return mcpSetupCommand(args[1:])
	}
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
	networkInterface := fs.String("interface", "", "bind local Xray outbound connections to this network interface")
	allowDynamicTargets := fs.Bool("allow-dynamic-targets", false, "allow MCP tool calls to supply direct SSH targets")
	maxConcurrent := fs.Int("max-concurrent-tests", 2, "maximum concurrent MCP requests")
	requestTimeout := fs.Duration("request-timeout", 10*time.Minute, "maximum duration of one MCP request")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *maxConcurrent < 1 || *requestTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "usage: xrayprobe mcp [--allow-path DIR] [--target NAME=SSH_ADDRESS] [--allow-dynamic-targets] [--max-concurrent-tests N] [--request-timeout D]")
		return 2
	}
	remoteTargets, err := remote.ParseTargets(targets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 2
	}
	server, err := mcpserver.New(mcpserver.Options{Manager: manager, AllowedRoots: roots, RemoteTargets: remoteTargets, AllowDynamicTargets: *allowDynamicTargets, DefaultInterface: *networkInterface, MaxConcurrent: *maxConcurrent, RequestTimeout: *requestTimeout})
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

// mcpSetupCommand registers xrayprobe as an MCP server for a supported AI
// client in one step, instead of the user hand-resolving an absolute binary
// path, hand-editing JSON, or hunting down a network interface name.
func mcpSetupCommand(args []string) int {
	if len(args) != 1 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stderr, "usage: xrayprobe mcp setup <claude-code|codex|claude-desktop>")
		return 2
	}
	target := args[0]

	binaryPath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe: could not resolve the xrayprobe binary path:", err)
		return 3
	}
	if resolved, err := filepath.EvalSymlinks(binaryPath); err == nil {
		binaryPath = resolved
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe: could not resolve your home directory:", err)
		return 3
	}
	configsDir := filepath.Join(home, ".xrayprobe", "configs")
	if err := os.MkdirAll(configsDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe: could not create", configsDir+":", err)
		return 3
	}

	networkInterface := ""
	if detected, err := netutil.DefaultInterfaceName(); err == nil && detected != "" {
		networkInterface = detected
		fmt.Println("detected default network interface:", detected)
	}

	switch target {
	case "claude-code":
		err = mcpSetupCLIClient("claude", binaryPath, configsDir, networkInterface)
	case "codex":
		err = mcpSetupCLIClient("codex", binaryPath, configsDir, networkInterface)
	case "claude-desktop":
		err = mcpSetupClaudeDesktop(binaryPath, configsDir, networkInterface)
	default:
		fmt.Fprintf(os.Stderr, "xrayprobe: unknown MCP client %q; supported: claude-code, codex, claude-desktop\n", target)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "xrayprobe:", err)
		return 3
	}
	return 0
}

// mcpServerArgs builds the arguments xrayprobe itself needs when launched as
// an MCP server, shared by every client's registration.
func mcpServerArgs(binaryPath, configsDir, networkInterface string) []string {
	args := []string{"mcp", "--allow-path", configsDir}
	if networkInterface != "" {
		args = append(args, "--interface", networkInterface)
	}
	return args
}

// mcpSetupCLIClient registers xrayprobe with an MCP client that itself
// exposes a CLI (`claude mcp add` / `codex mcp add`), so the user runs one
// xrayprobe command instead of resolving a path and typing that command
// themselves. Falls back to printing the exact command if the client's CLI
// isn't on PATH.
func mcpSetupCLIClient(clientBinary, binaryPath, configsDir, networkInterface string) error {
	registerArgs := append([]string{"mcp", "add", "xrayprobe", "--", binaryPath}, mcpServerArgs(binaryPath, configsDir, networkInterface)...)
	if _, err := exec.LookPath(clientBinary); err != nil {
		fmt.Printf("could not find '%s' on PATH; run this yourself once it's installed:\n  %s %s\n", clientBinary, clientBinary, strings.Join(registerArgs, " "))
		return nil
	}
	cmd := exec.Command(clientBinary, registerArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", clientBinary, strings.Join(registerArgs, " "), err)
	}
	return nil
}

// mcpSetupClaudeDesktop merges an xrayprobe entry into Claude Desktop's
// mcpServers config, preserving any other servers or settings already
// there, instead of the user hand-editing JSON in the right OS-specific
// location and getting the syntax right.
func mcpSetupClaudeDesktop(binaryPath, configsDir, networkInterface string) error {
	configPath, err := claudeDesktopConfigPath()
	if err != nil {
		return err
	}

	root := map[string]json.RawMessage{}
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("existing Claude Desktop config at %s is not valid JSON: %w", configPath, err)
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("existing mcpServers in %s is not valid JSON: %w", configPath, err)
		}
	}

	entry, err := json.Marshal(map[string]any{"command": binaryPath, "args": mcpServerArgs(binaryPath, configsDir, networkInterface)})
	if err != nil {
		return err
	}
	servers["xrayprobe"] = entry

	serversBytes, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	root["mcpServers"] = serversBytes

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, out, 0o600); err != nil {
		return err
	}
	fmt.Println("updated", configPath)
	fmt.Println("restart Claude Desktop to load the xrayprobe MCP server.")
	return nil
}

func claudeDesktopConfigPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("%%APPDATA%% is not set")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
	default:
		return "", fmt.Errorf("Claude Desktop is not available on %s; use 'xrayprobe mcp setup claude-code' or 'xrayprobe mcp setup codex' instead", runtime.GOOS)
	}
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
	networkInterface := fs.String("interface", "", "bind Xray outbound connections to this network interface")
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
		if fs.NArg() > 1 && looksLikeShareLink(fs.Arg(0)) {
			fmt.Fprintln(os.Stderr, "xrayprobe: your shell split the share link into multiple arguments.")
			fmt.Fprintln(os.Stderr, "Share links contain '&', '?', and '#', which shells treat specially unless")
			fmt.Fprintln(os.Stderr, "the whole link is quoted. Wrap it in single quotes, e.g.:")
			fmt.Fprintf(os.Stderr, "  xrayprobe test '%s...'\n", fs.Arg(0))
		} else {
			fmt.Fprintln(os.Stderr, "usage: xrayprobe test [options] <URI|file|subscription-URL>")
		}
		return 2
	}
	if *concurrency < 1 || *attempts < 1 || *maxConfigs < 1 {
		fmt.Fprintln(os.Stderr, "concurrency, attempts, and max-configs must be positive")
		return 2
	}
	var progressMu sync.Mutex
	runOptions := types.RunOptions{
		CoreVersion: *coreVersion, OutboundTag: *outbound, Interface: *networkInterface, Concurrency: *concurrency, MaxConfigs: *maxConfigs,
		Probe: types.ProbeOptions{Attempts: *attempts, Timeout: *timeout, ProbeURL: *probeURL, MetadataURL: *metadataURL, NoMetadata: *noMetadata, Speed: *speed, DownloadBytes: *downloadBytes},
		Progress: func(event types.ProgressEvent) {
			progressMu.Lock()
			defer progressMu.Unlock()
			printProgress(event)
		},
	}
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

func looksLikeShareLink(value string) bool {
	value = strings.ToLower(value)
	for _, scheme := range []string{"vless://", "vmess://", "trojan://", "ss://"} {
		if strings.HasPrefix(value, scheme) {
			return true
		}
	}
	return false
}

func printProgress(event types.ProgressEvent) {
	label := event.Name
	if label == "" {
		label = fmt.Sprintf("config %d", event.Index+1)
	}
	switch event.Phase {
	case "start":
		if event.Total > 1 {
			fmt.Fprintf(os.Stderr, "[%d/%d] testing %s...\n", event.Index+1, event.Total, label)
		} else {
			fmt.Fprintf(os.Stderr, "testing %s...\n", label)
		}
	case "done":
		if event.Status == "ok" {
			fmt.Fprintf(os.Stderr, "  %s: ok (score %.0f, %s)\n", label, event.Score, event.Grade)
		} else {
			fmt.Fprintf(os.Stderr, "  %s: failed - %s\n", label, event.Error)
		}
	}
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
  xrayprobe mcp setup <claude-code|codex|claude-desktop>
  xrayprobe version

Examples:
  xrayprobe test 'vless://...'
  xrayprobe test ./config.json --format json
  xrayprobe test https://example.com/subscription.txt --concurrency 4
  xrayprobe core install v26.3.27
  xrayprobe mcp setup claude-code

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
  --interface NAME              Bind Xray outbound connections to this interface
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
  xrayprobe mcp setup <claude-code|codex|claude-desktop>

Run the local stdio MCP server for AI clients. File inputs are limited to the
current working directory unless one or more --allow-path directories are set.

"xrayprobe mcp setup <client>" registers xrayprobe with that client for you:
it resolves this binary's absolute path, detects your default network
interface, creates a configs directory under ~/.xrayprobe, and either runs
the client's own "mcp add" command (claude-code, codex) or merges the entry
into Claude Desktop's config file directly (claude-desktop) - no manual path
lookup or hand-edited JSON required.

Options:
  --allow-path DIR             Allow MCP file inputs under DIR (repeatable)
  --target NAME=SSH_ADDRESS    Configure a remote probe target (repeatable)
  --interface NAME              Bind local Xray outbound connections to this interface
  --allow-dynamic-targets      Allow tool calls to supply direct SSH targets
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
