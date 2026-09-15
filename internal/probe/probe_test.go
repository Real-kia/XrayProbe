package probe

import (
	"testing"

	"github.com/Real-kia/XrayProbe/internal/types"
)

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
