package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/Real-kia/XrayProbe/internal/types"
)

func Render(w io.Writer, results []types.Result, format string, ranked bool) error {
	copyResults := append([]types.Result(nil), results...)
	if ranked {
		sort.SliceStable(copyResults, func(i, j int) bool {
			if copyResults[i].Status != copyResults[j].Status {
				return copyResults[i].Status == "ok"
			}
			return copyResults[i].Score > copyResults[j].Score
		})
	}
	switch strings.ToLower(format) {
	case "json":
		return json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "results": copyResults})
	case "csv":
		return csvOutput(w, copyResults)
	case "table", "":
		return tableOutput(w, copyResults)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func tableOutput(w io.Writer, results []types.Result) error {
	headers := []string{"NAME", "PROTO", "IP", "LOCATION", "SUCCESS", "MEDIAN", "JITTER", "SCORE", "GRADE", "STATUS"}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		ip, location, success, median, jitter := "-", "-", "-", "-", "-"
		if result.Outbound != nil && result.Outbound.IP != "" {
			ip = result.Outbound.IP
			if trimmed := strings.Trim(strings.TrimSpace(result.Outbound.City+", "+result.Outbound.Country), ", "); trimmed != "" {
				location = trimmed
			}
		}
		if result.Metrics != nil {
			success = fmt.Sprintf("%.0f%%", result.Metrics.SuccessRate)
			median = fmt.Sprintf("%.0fms", result.Metrics.MedianLatencyMS)
			jitter = fmt.Sprintf("%.0fms", result.Metrics.JitterMS)
		}
		status := result.Status
		if result.Error != "" {
			status += ": " + result.Error
		}
		rows = append(rows, []string{
			safe(result.Name), result.Protocol, ip, location, success, median, jitter,
			fmt.Sprintf("%.1f", result.Score), result.Grade, safe(status),
		})
	}
	return writeBoxTable(w, headers, rows)
}

// writeBoxTable renders a bordered table (like `docker ps` or a SQL client),
// with column widths computed from actual content so columns line up
// regardless of how long any given cell is - unlike a raw tab-separated
// table, which only lines up if every preceding cell happens to land on the
// terminal's fixed tab stops.
func writeBoxTable(w io.Writer, headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if width := displayWidth(cell); width > widths[i] {
				widths[i] = width
			}
		}
	}

	var b strings.Builder
	writeBorder(&b, widths, '┌', '┬', '┐')
	writeRow(&b, headers, widths)
	writeBorder(&b, widths, '├', '┼', '┤')
	for _, row := range rows {
		writeRow(&b, row, widths)
	}
	writeBorder(&b, widths, '└', '┴', '┘')
	_, err := io.WriteString(w, b.String())
	return err
}

func writeBorder(b *strings.Builder, widths []int, left, mid, right rune) {
	b.WriteRune(left)
	for i, width := range widths {
		b.WriteString(strings.Repeat("─", width+2))
		if i < len(widths)-1 {
			b.WriteRune(mid)
		}
	}
	b.WriteRune(right)
	b.WriteByte('\n')
}

func writeRow(b *strings.Builder, cells []string, widths []int) {
	b.WriteRune('│')
	for i, cell := range cells {
		b.WriteByte(' ')
		b.WriteString(cell)
		if pad := widths[i] - displayWidth(cell); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteByte(' ')
		b.WriteRune('│')
	}
	b.WriteByte('\n')
}

// displayWidth approximates the terminal column width of s, treating CJK
// and most emoji as double-width instead of counting runes 1-for-1. It's a
// best-effort table: a flag emoji (two combined "regional indicator" code
// points) renders as one glyph in some terminals and two in others, so exact
// alignment for those specifically can't be guaranteed across every
// terminal - but this keeps ordinary text and most other wide characters
// aligned correctly.
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		width += runeWidth(r)
	}
	return width
}

func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case (r >= 0x1100 && r <= 0x115F), // Hangul Jamo
		r == 0x2329, r == 0x232A,
		(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F), // CJK ... Yi
		(r >= 0xAC00 && r <= 0xD7A3),                // Hangul Syllables
		(r >= 0xF900 && r <= 0xFAFF),                // CJK Compatibility Ideographs
		(r >= 0xFE30 && r <= 0xFE6F),                // CJK Compatibility Forms
		(r >= 0xFF00 && r <= 0xFF60),                // Fullwidth Forms
		(r >= 0xFFE0 && r <= 0xFFE6),
		(r >= 0x1F1E6 && r <= 0x1F1FF), // regional indicator symbols (flags)
		(r >= 0x1F300 && r <= 0x1FAFF), // most emoji blocks
		(r >= 0x20000 && r <= 0x3FFFD):
		return 2
	default:
		return 1
	}
}

func csvOutput(w io.Writer, results []types.Result) error {
	c := csv.NewWriter(w)
	if err := c.Write([]string{"index", "name", "protocol", "transport", "security", "core_version", "status", "ip", "country", "city", "asn", "success_rate", "median_latency_ms", "jitter_ms", "score", "grade", "error"}); err != nil {
		return err
	}
	for _, result := range results {
		var ip, country, city, asn, success, median, jitter string
		if result.Outbound != nil {
			ip, country, city, asn = result.Outbound.IP, result.Outbound.Country, result.Outbound.City, result.Outbound.ASN
		}
		if result.Metrics != nil {
			success = fmt.Sprintf("%.2f", result.Metrics.SuccessRate)
			median = fmt.Sprintf("%.2f", result.Metrics.MedianLatencyMS)
			jitter = fmt.Sprintf("%.2f", result.Metrics.JitterMS)
		}
		row := []string{strconv.Itoa(result.Index), result.Name, result.Protocol, result.Transport, result.Security, result.Core, result.Status, ip, country, city, asn, success, median, jitter, fmt.Sprintf("%.2f", result.Score), result.Grade, result.Error}
		if err := c.Write(row); err != nil {
			return err
		}
	}
	c.Flush()
	return c.Error()
}

func safe(value string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(value)
}
