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
	if _, err := fmt.Fprintln(w, "NAME\tPROTO\tIP\tLOCATION\tSUCCESS\tMEDIAN\tJITTER\tSCORE\tGRADE\tSTATUS"); err != nil {
		return err
	}
	for _, result := range results {
		ip, location, success, median, jitter := "-", "-", "-", "-", "-"
		if result.Outbound != nil {
			ip = result.Outbound.IP
			location = strings.TrimSpace(result.Outbound.City + ", " + result.Outbound.Country)
			location = strings.Trim(location, ", ")
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
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%.1f\t%s\t%s\n", safe(result.Name), result.Protocol, ip, location, success, median, jitter, result.Score, result.Grade, safe(status)); err != nil {
			return err
		}
	}
	return nil
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
