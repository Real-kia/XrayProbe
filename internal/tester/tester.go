package tester

import (
	"context"
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

func Run(ctx context.Context, manager *core.Manager, source string, options types.RunOptions) ([]types.Result, string, error) {
	specs, err := input.Load(ctx, source, options.MaxConfigs)
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
	binary, version, err := manager.Ensure(ctx, options.CoreVersion)
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
		jobs <- spec
	}
	close(jobs)
	wg.Wait()
	return results, version, nil
}

func runOne(ctx context.Context, binary, version string, spec types.Spec, options types.RunOptions) types.Result {
	result := types.Result{Index: spec.Index, Name: spec.Name, Core: version, Status: "failed"}
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
