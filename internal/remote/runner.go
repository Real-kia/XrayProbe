package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Real-kia/XrayProbe/internal/input"
	"github.com/Real-kia/XrayProbe/internal/types"
	"github.com/Real-kia/XrayProbe/internal/version"
)

const (
	defaultRequestTimeout = 10 * time.Minute
	maxWorkerOutput       = 32 << 20
)

type Target struct {
	Name    string
	Address string
}

type Runner struct {
	targets      map[string]Target
	timeout      time.Duration
	allowDynamic bool
}

type WorkerRequest struct {
	Source  input.Source     `json:"source"`
	Options types.RunOptions `json:"options"`
}

type WorkerResponse struct {
	Results     []types.Result `json:"results,omitempty"`
	CoreVersion string         `json:"core_version,omitempty"`
	Error       string         `json:"error,omitempty"`
}

func New(targets []Target, timeout time.Duration, allowDynamic bool) (*Runner, error) {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	result := &Runner{targets: make(map[string]Target, len(targets)), timeout: timeout, allowDynamic: allowDynamic}
	for _, target := range targets {
		name := strings.TrimSpace(target.Name)
		address := strings.TrimSpace(target.Address)
		if err := validateTarget(name, address); err != nil {
			return nil, err
		}
		if _, exists := result.targets[name]; exists {
			return nil, fmt.Errorf("remote target %q is configured more than once", name)
		}
		result.targets[name] = Target{Name: name, Address: address}
	}
	return result, nil
}

func ParseTargets(values []string) ([]Target, error) {
	targets := make([]Target, 0, len(values))
	for _, value := range values {
		name, address, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("remote target %q must use NAME=SSH_ADDRESS", value)
		}
		targets = append(targets, Target{Name: name, Address: address})
	}
	return targets, nil
}

func (r *Runner) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.targets))
	for name := range r.targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Runner) Run(ctx context.Context, name string, source input.Source, options types.RunOptions) ([]types.Result, string, error) {
	if r == nil {
		return nil, "", errors.New("remote runner is not configured")
	}
	target, err := r.target(name)
	if err != nil {
		return nil, "", err
	}
	options.AllowedRoots = nil
	options.RestrictPaths = false
	payload, err := json.Marshal(WorkerRequest{Source: source, Options: options})
	if err != nil {
		return nil, "", fmt.Errorf("encode remote test request: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = maxWorkerOutput, 1<<20
	cmd := exec.CommandContext(requestCtx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", target.Address, "sh -c "+shellQuote(bootstrapScript()))
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if requestCtx.Err() != nil {
			return nil, "", requestCtx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, "", fmt.Errorf("remote target %q: %s", name, message)
	}
	var response WorkerResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, "", fmt.Errorf("decode response from remote target %q: %w", name, err)
	}
	if response.Error != "" {
		return nil, response.CoreVersion, errors.New(response.Error)
	}
	return response.Results, response.CoreVersion, nil
}

func (r *Runner) target(name string) (Target, error) {
	if target, ok := r.targets[name]; ok {
		return target, nil
	}
	if !r.allowDynamic {
		return Target{}, fmt.Errorf("remote target %q is not configured; enable --allow-dynamic-targets or configure --target NAME=SSH_ADDRESS", name)
	}
	if err := validateAddress(name); err != nil {
		return Target{}, fmt.Errorf("dynamic remote target %q: %w", name, err)
	}
	return Target{Name: name, Address: name}, nil
}

func bootstrapScript() string {
	release := "v" + version.Value
	return fmt.Sprintf(`set -eu
bin="${HOME}/.local/bin/xrayprobe"
current=""
if [ -x "$bin" ]; then
  current="$($bin version 2>/dev/null || true)"
fi
if [ "$current" != "xrayprobe %s" ]; then
  command -v curl >/dev/null 2>&1 || { echo "remote bootstrap requires curl" >&2; exit 1; }
  XRAYPROBE_VERSION=%s XRAYPROBE_INSTALL_DIR="${HOME}/.local/bin" sh -c "$(curl -fsSL https://raw.githubusercontent.com/Real-kia/XrayProbe/main/scripts/install.sh)" >&2
fi
exec "$bin" remote-worker`, version.Value, release)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func validName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func validateTarget(name, address string) error {
	if !validName(name) {
		return fmt.Errorf("remote target name %q is invalid", name)
	}
	if name == "local" {
		return errors.New("remote target name local is reserved")
	}
	if err := validateAddress(address); err != nil {
		return fmt.Errorf("remote target %q: %w", name, err)
	}
	return nil
}

func validateAddress(address string) error {
	if address == "" || strings.HasPrefix(address, "-") || strings.IndexFunc(address, func(r rune) bool { return r <= ' ' }) >= 0 {
		return errors.New("invalid SSH address")
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.overflow = true
		return 0, errors.New("remote command output exceeded its limit")
	}
	return b.Buffer.Write(p)
}
