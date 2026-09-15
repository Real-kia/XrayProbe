package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/Real-kia/XrayProbe/internal/types"
)

func TestClassifyErrorRecognizesKnownCauses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "unknown error"},
		{"probe error", &ProbeError{Reason: "connection refused by the destination"}, "connection refused by the destination"},
		{"deadline exceeded", fmt.Errorf("wrap: %w", context.DeadlineExceeded), "timed out waiting for a response"},
		{"dns not found", &net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}, "DNS lookup failed for example.invalid (no such host)"},
		{"dns timeout", &net.DNSError{Err: "timeout", Name: "example.invalid", IsTimeout: true}, "DNS lookup timed out"},
		{"connection refused", &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, "connection refused"},
		{"connection reset", &net.OpError{Op: "read", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}, "connection reset by the destination after connecting"},
		{"host unreachable", &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.EHOSTUNREACH}}, "host unreachable"},
		{"unrecognized", errors.New("some other failure"), "some other failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Errorf("ClassifyError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSocksReplyReasonCoversAllReplyCodes(t *testing.T) {
	for code := byte(1); code <= 8; code++ {
		if reason := socksReplyReason(code); reason == "" {
			t.Errorf("socksReplyReason(%d) returned an empty reason", code)
		}
	}
	if reason := socksReplyReason(99); reason == "" {
		t.Errorf("socksReplyReason(99) should still return a fallback reason")
	}
}

func TestSummarize(t *testing.T) {
	m := summarize(5, []float64{100, 120, 110, 130})
	if m.Successful != 4 || m.SuccessRate != 80 {
		t.Fatalf("unexpected success metrics: %+v", m)
	}
	if m.MinLatencyMS != 100 || m.MedianLatencyMS != 115 {
		t.Fatalf("unexpected latency metrics: %+v", m)
	}
	if m.JitterMS <= 0 {
		t.Fatalf("expected jitter: %+v", m)
	}
}

func TestScoreAndGrade(t *testing.T) {
	m := &types.Metrics{Attempts: 5, SuccessRate: 100, MedianLatencyMS: 20, JitterMS: 2}
	score := Score(m)
	if score < 95 || Grade(score) != "Excellent" {
		t.Fatalf("unexpected score %.2f (%s)", score, Grade(score))
	}
	if Grade(70) != "Good" || Grade(50) != "Fair" || Grade(49.9) != "Poor" {
		t.Fatal("grade boundaries are incorrect")
	}
}
