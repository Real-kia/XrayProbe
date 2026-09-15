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

// parseBoxRow splits one bordered-table data row ("│ a │ b │ c │") into its
// trimmed cell values.
func parseBoxRow(line string) []string {
	trimmed := strings.Trim(line, "│")
	parts := strings.Split(trimmed, "│")
	cells := make([]string, len(parts))
	for i, part := range parts {
		cells[i] = strings.TrimSpace(part)
	}
	return cells
}

func TestRenderTableRanksOkAboveFailedAndByScore(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleResults(), "table", true); err != nil {
		t.Fatalf("Render: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// top border, header, separator, 3 data rows, bottom border
	if len(lines) != 7 {
		t.Fatalf("expected 7 lines (borders + header + 3 rows), got %d lines: %q", len(lines), lines)
	}
	header := parseBoxRow(lines[1])
	if header[0] != "NAME" || header[len(header)-1] != "STATUS" {
		t.Fatalf("unexpected header row: %v", header)
	}
	row1 := parseBoxRow(lines[3])
	if row1[0] != "best with tabs" {
		t.Errorf("expected highest-scoring ok result first, got %q", row1[0])
	}
	row2 := parseBoxRow(lines[4])
	if row2[0] != "slow" {
		t.Errorf("expected lower-scoring ok result second, got %q", row2[0])
	}
	row3 := parseBoxRow(lines[5])
	status := row3[len(row3)-1]
	if status != "failed: dial failed with a newline" {
		t.Errorf("expected failed result last with sanitized error, got %q", status)
	}
	for i, line := range lines {
		if strings.ContainsAny(line, "\t\n\r") {
			t.Errorf("line %d contains a raw tab/newline that should have been sanitized: %q", i, line)
		}
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

func TestRenderTableShowsPlaceholderWhenMetadataLookupFailed(t *testing.T) {
	results := []types.Result{
		{
			Index: 0, ConfigID: "cfg_nometa", Name: "nometa", Protocol: "vless", Core: "v26.3.27",
			Status: "ok", Score: 88, Grade: "Excellent",
			// A failed metadata lookup leaves a non-nil but empty Outbound
			// (see probe.Run), which must still render as "-", not blank.
			Outbound: &types.OutboundInfo{},
			Metrics:  &types.Metrics{SuccessRate: 100, MedianLatencyMS: 50, JitterMS: 1},
		},
	}
	var buf bytes.Buffer
	if err := Render(&buf, results, "table", false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// top border, header, separator, 1 data row, bottom border
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines (borders + header + 1 row), got %d lines: %q", len(lines), lines)
	}
	fields := parseBoxRow(lines[3])
	if fields[2] != "-" {
		t.Errorf("IP column = %q, want %q for a failed metadata lookup", fields[2], "-")
	}
	if fields[3] != "-" {
		t.Errorf("LOCATION column = %q, want %q for a failed metadata lookup", fields[3], "-")
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleResults(), "yaml", false); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}
