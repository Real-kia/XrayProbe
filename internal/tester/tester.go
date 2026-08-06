package tester

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/KiaTheRandomGuy/XrayProbe/internal/core"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/input"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/probe"
	"github.com/KiaTheRandomGuy/XrayProbe/internal/types"
	xrayconfig "github.com/KiaTheRandomGuy/XrayProbe/internal/xray"
)

type Service struct {
	Manager *core.Manager
}

func NewService(manager *core.Manager) *Service {
	return &Service{Manager: manager}
}

func Run(ctx context.Context, manager *core.Manager, source string, options types.RunOptions) ([]types.Result, string, error) {
	return NewService(manager).Run(ctx, input.Source{Value: source, Kind: "auto"}, options)
}

func (s *Service) Run(ctx context.Context, source input.Source, options types.RunOptions) ([]types.Result, string, error) {
	specs, err := input.LoadSource(ctx, source, input.LoadOptions{MaxConfigs: options.MaxConfigs, AllowedRoots: options.AllowedRoots, RestrictPaths: options.RestrictPaths})
	if err != nil {
		return nil, "", err
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 4
	}
	if options.Concurrency > len(specs) {
		options.Concurrency = len(specs)
	}
	if options.Probe.Attempts <= 0 {
		options.Probe.Attempts = 5
	}
	if options.Probe.Timeout <= 0 {
		options.Probe.Timeout = 10 * time.Second
	}
	if s == nil || s.Manager == nil {
		return nil, "", errors.New("XrayProbe service has no core manager")
	}
	binary, version, err := s.Manager.Ensure(ctx, options.CoreVersion)
	if err != nil {
		return nil, "", fmt.Errorf("install Xray-core: %w", err)
	}

	results := make([]types.Result, len(specs))
	jobs := make(chan types.Spec)
	var wg sync.WaitGroup
	for i := 0; i < options.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for spec := range jobs {
				results[spec.Index] = runOne(ctx, binary, version, spec, options)
			}
		}()
	}
	for _, spec := range specs {
		select {
		case jobs <- spec:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results, version, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return results, version, nil
}

func runOne(ctx context.Context, binary, version string, spec types.Spec, options types.RunOptions) types.Result {
	result := types.Result{Index: spec.Index, ConfigID: configID(spec), Name: spec.Name, Core: version, Status: "failed"}
	start := time.Now()
	port, err := freePort()
	if err != nil {
		result.Error = "could not allocate a local probe port"
		return result
	}
	config, metadata, err := xrayconfig.Build(spec, port, options.OutboundTag)
	if err != nil {
		result.Error = cleanError(err)
		return result
	}
	result.Protocol, result.Transport, result.Security = metadata.Protocol, metadata.Transport, metadata.Security
	err = xrayconfig.Run(ctx, binary, config, port, func(probeCtx context.Context, address string) error {
		outbound, metrics, probeErr := probe.Run(probeCtx, address, options.Probe)
		result.Outbound, result.Metrics = outbound, metrics
		if metrics != nil {
			result.Score = probe.Score(metrics)
			result.Grade = probe.Grade(result.Score)
		}
		return probeErr
	})
	result.DurationMS = time.Since(start).Milliseconds()
	if err == nil {
		result.Status = "ok"
	} else {
		result.Error = cleanError(err)
	}
	return result
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("local listener did not return TCP address")
	}
	return address.Port, nil
}

func cleanError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func configID(spec types.Spec) string {
	value := spec.URI
	if value == "" {
		value = spec.Source + ":" + spec.Name
	}
	sum := sha256.Sum256([]byte(value))
	return "cfg_" + hex.EncodeToString(sum[:])[:12]
}
