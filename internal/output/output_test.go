package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Real-kia/XrayProbe/internal/types"
)

func sampleResults() []types.Result {
	return []types.Result{
		{
			Index: 0, ConfigID: "cfg_slow", Name: "slow", Protocol: "vless", Core: "v26.3.27",
			Status: "ok", Score: 70, Grade: "Fair",
			Outbound: &types.OutboundInfo{IP: "1.1.1.1", Country: "US", City: "Ashburn"},
			Metrics:  &types.Metrics{SuccessRate: 100, MedianLatencyMS: 120, JitterMS: 5},
		},
		{
			Index: 1, ConfigID: "cfg_best", Name: "best\twith\ttabs", Protocol: "vmess", Core: "v26.3.27",
			Status: "ok", Score: 95, Grade: "Excellent",
			Outbound: &types.OutboundInfo{IP: "2.2.2.2", Country: "DE"},
			Metrics:  &types.Metrics{SuccessRate: 100, MedianLatencyMS: 40, JitterMS: 2},
		},
		{
			Index: 2, ConfigID: "cfg_bad", Name: "bad", Protocol: "trojan", Core: "v26.3.27",
			Status: "failed", Error: "dial failed\nwith a newline",
		},
	}
}

func TestRenderTableRanksOkAboveFailedAndByScore(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleResults(), "table", true); err != nil {
		t.Fatalf("Render: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + 3 rows, got %d lines: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[1], "best with tabs") {
		t.Errorf("expected highest-scoring ok result first, got %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "slow") {
		t.Errorf("expected lower-scoring ok result second, got %q", lines[2])
	}
	if !strings.Contains(lines[3], "failed: dial failed with a newline") {
		t.Errorf("expected failed result last with sanitized error, got %q", lines[3])
	}
	if strings.ContainsAny(lines[1], "\t\n\r") && strings.Count(lines[1], "\t") != 9 {
		// The table format itself uses tabs as column separators; only the
		// embedded name/error values must have their own tabs/newlines stripped.
		t.Errorf("unexpected raw tab/newline leaking from a field value: %q", lines[1])
	}
}

func TestRenderJSONIncludesSchemaVersionAndResults(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleResults(), "json", false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded struct {
		SchemaVersion int            `json:"schema_version"`
		Results       []types.Result `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", decoded.SchemaVersion)
	}
	if len(decoded.Results) != 3 {
		t.Errorf("len(results) = %d, want 3", len(decoded.Results))
	}
	// ranked=false must preserve input order.
	if decoded.Results[0].ConfigID != "cfg_slow" {
		t.Errorf("expected unranked JSON to preserve input order, got %q first", decoded.Results[0].ConfigID)
	}
}

func TestRenderCSVRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleResults(), "csv", false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected header + 3 rows, got %d", len(rows))
	}
	if rows[1][1] != "slow" {
		t.Errorf("row 1 name = %q, want %q", rows[1][1], "slow")
	}
	if rows[3][6] != "failed" || rows[3][len(rows[3])-1] != "dial failed\nwith a newline" {
		t.Errorf("failed row not preserved verbatim: %#v", rows[3])
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleResults(), "yaml", false); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}
