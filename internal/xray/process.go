package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type ProbeFunc func(context.Context, string) error

func Run(ctx context.Context, binary string, config []byte, port int, probe ProbeFunc) error {
	dir, err := os.MkdirTemp("", "xrayprobe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		return err
	}

	validation := exec.CommandContext(ctx, binary, "run", "-test", "-c", configPath)
	validation.Stdout = &bytes.Buffer{}
	validation.Stderr = &bytes.Buffer{}
	if err := validation.Run(); err != nil {
		return errors.New("Xray rejected the generated configuration")
	}

	process := exec.Command(binary, "run", "-c", configPath)
	process.Stdout = &bytes.Buffer{}
	process.Stderr = &bytes.Buffer{}
	if err := process.Start(); err != nil {
		return fmt.Errorf("start Xray: %w", err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- process.Wait() }()
	address := fmt.Sprintf("127.0.0.1:%d", port)
	if err := waitForPort(ctx, address, waitErr); err != nil {
		_ = process.Process.Kill()
		select {
		case <-waitErr:
		case <-time.After(2 * time.Second):
		}
		return err
	}
	probeErr := probe(ctx, address)
	_ = process.Process.Kill()
	select {
	case <-waitErr:
	case <-time.After(2 * time.Second):
		_ = process.Process.Kill()
	}
	return probeErr
}

func waitForPort(ctx context.Context, address string, processDone <-chan error) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		conn, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case err := <-processDone:
			if err == nil {
				return errors.New("Xray exited before opening its local proxy")
			}
			return errors.New("Xray exited before opening its local proxy")
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for Xray local proxy")
		case <-ticker.C:
		}
	}
}
