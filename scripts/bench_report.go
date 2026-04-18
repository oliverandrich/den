// bench_report reads `go test -bench` output on stdin and splices the
// benchmark results into README.md between the sentinel markers
//
//	<!-- BENCH:SERIAL -->   ... <!-- /BENCH:SERIAL -->
//	<!-- BENCH:CONCURRENT --> ... <!-- /BENCH:CONCURRENT -->
//
// Run via `just bench-readme`. The tool intentionally does no statistics —
// callers who want significance testing should use benchstat separately.
//
//go:build ignore

package main

import (
	"bufio"
	"cmp"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type result struct {
	scenario string  // e.g. "Insert", "FindByID"
	backend  string  // "SQLite" or "Postgres"
	nsPerOp  float64 // average ns/op
	allocs   int64   // allocs/op
}

// Go-bench output example:
// BenchmarkRW_SQLite_Insert-14    	    9015	    136223 ns/op	    2890 B/op	      31 allocs/op
var benchLine = regexp.MustCompile(`^Benchmark(RW|Concurrent)_(SQLite|Postgres)_(\S+?)(?:-\d+)?\s+\d+\s+([\d.]+)\s+ns/op(?:\s+\d+\s+B/op)?(?:\s+(\d+)\s+allocs/op)?`)

func parse(r io.Reader) (serial, concurrent []result, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		m := benchLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		kind, backend, scenario, nsStr, allocStr := m[1], m[2], m[3], m[4], m[5]
		ns, parseErr := strconv.ParseFloat(nsStr, 64)
		if parseErr != nil {
			continue
		}
		var allocs int64
		if allocStr != "" {
			allocs, _ = strconv.ParseInt(allocStr, 10, 64)
		}
		res := result{scenario: scenario, backend: backend, nsPerOp: ns, allocs: allocs}
		switch kind {
		case "RW":
			serial = append(serial, res)
		case "Concurrent":
			concurrent = append(concurrent, res)
		}
	}
	return serial, concurrent, sc.Err()
}

// groupByScenario joins SQLite and Postgres results for the same scenario
// in the input's scenario order, so the table follows the benchmark file
// rather than alphabetical sorting.
func groupByScenario(rs []result) [][2]*result {
	seen := make(map[string]int)
	var order []string
	for _, r := range rs {
		if _, ok := seen[r.scenario]; !ok {
			seen[r.scenario] = len(order)
			order = append(order, r.scenario)
		}
	}
	out := make([][2]*result, len(order))
	for i := range rs {
		idx := seen[rs[i].scenario]
		switch rs[i].backend {
		case "SQLite":
			out[idx][0] = &rs[i]
		case "Postgres":
			out[idx][1] = &rs[i]
		}
	}
	return out
}

func formatDuration(ns float64) string {
	switch {
	case ns >= 1e6:
		return fmt.Sprintf("%.2f ms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.1f µs", ns/1e3)
	default:
		return fmt.Sprintf("%.0f ns", ns)
	}
}

// formatThroughput converts average ns/op into a human ops/sec figure for
// concurrent benchmarks. Uses 1e9 / ns.
func formatThroughput(ns float64) string {
	ops := 1e9 / ns
	switch {
	case ops >= 1e6:
		return fmt.Sprintf("%.2fM ops/s", ops/1e6)
	case ops >= 1e3:
		return fmt.Sprintf("%.1fk ops/s", ops/1e3)
	default:
		return fmt.Sprintf("%.0f ops/s", ops)
	}
}

func renderSerial(rs []result) string {
	groups := groupByScenario(rs)
	slices.SortStableFunc(groups, func(a, b [2]*result) int {
		// Preserve input order — groupByScenario already did that, this is
		// purely to make the sort deterministic under later refactors.
		return cmp.Compare(scenarioOrderKey(a), scenarioOrderKey(b))
	})
	var sb strings.Builder
	sb.WriteString("| Scenario | SQLite | Postgres | SQLite allocs | Postgres allocs |\n")
	sb.WriteString("|---|---:|---:|---:|---:|\n")
	for _, g := range groups {
		name := ""
		if g[0] != nil {
			name = g[0].scenario
		} else if g[1] != nil {
			name = g[1].scenario
		}
		sb.WriteString("| " + humanScenario(name) + " | ")
		sb.WriteString(cell(g[0], formatDuration) + " | ")
		sb.WriteString(cell(g[1], formatDuration) + " | ")
		sb.WriteString(cellAllocs(g[0]) + " | ")
		sb.WriteString(cellAllocs(g[1]) + " |\n")
	}
	return sb.String()
}

func renderConcurrent(rs []result) string {
	groups := groupByScenario(rs)
	var sb strings.Builder
	sb.WriteString("| Scenario | SQLite | Postgres |\n")
	sb.WriteString("|---|---:|---:|\n")
	for _, g := range groups {
		name := ""
		if g[0] != nil {
			name = g[0].scenario
		} else if g[1] != nil {
			name = g[1].scenario
		}
		sb.WriteString("| " + humanScenario(name) + " | ")
		sb.WriteString(cell(g[0], formatThroughput) + " | ")
		sb.WriteString(cell(g[1], formatThroughput) + " |\n")
	}
	return sb.String()
}

func cell(r *result, fmtFn func(float64) string) string {
	if r == nil {
		return "—"
	}
	return fmtFn(r.nsPerOp)
}

func cellAllocs(r *result) string {
	if r == nil {
		return "—"
	}
	return fmt.Sprintf("%d", r.allocs)
}

// scenarioOrderKey only exists to keep the sort stable if someone later
// introduces a partial order; by default it returns an empty key so the
// original grouping order is preserved.
func scenarioOrderKey([2]*result) string { return "" }

// humanScenario maps benchmark function suffixes to readable labels.
func humanScenario(s string) string {
	switch s {
	case "Insert":
		return "Insert (single)"
	case "InsertMany100":
		return "InsertMany (100)"
	case "InsertMany1000":
		return "InsertMany (1000)"
	case "FindByID":
		return "FindByID"
	case "FindByIDs10":
		return "FindByIDs (10)"
	case "QueryFiltered10":
		return "Query + Sort + Limit(10)"
	case "QueryFiltered100":
		return "Query + Sort + Limit(100)"
	case "Iter1000":
		return "Iter (1000 rows)"
	case "CountFiltered":
		return "Count(filter)"
	case "SumFiltered":
		return "Sum(filter)"
	case "Search":
		return "FTS Search"
	case "WithFetchLinks":
		return "WithFetchLinks (20 rows)"
	case "Update":
		return "Update (single)"
	case "BulkUpdate100":
		return "QuerySet.Update (100)"
	case "Transaction":
		return "RunInTransaction"
	case "Mixed8020":
		return "Mixed reads/writes 80/20"
	case "QueueConsumer":
		return "Queue consumer (SkipLocked)"
	default:
		return s
	}
}

// splice replaces the content between start and end sentinel markers in
// body. Returns an error if either sentinel is missing or out of order.
func splice(body, startMarker, endMarker, replacement string) (string, error) {
	start := strings.Index(body, startMarker)
	if start < 0 {
		return "", fmt.Errorf("missing start marker %q", startMarker)
	}
	end := strings.Index(body, endMarker)
	if end < 0 {
		return "", fmt.Errorf("missing end marker %q", endMarker)
	}
	if end < start {
		return "", fmt.Errorf("%q appears before %q", endMarker, startMarker)
	}
	return body[:start+len(startMarker)] + "\n" + replacement + "\n" + body[end:], nil
}

func main() {
	readmePath := flag.String("readme", "README.md", "path to README.md to update")
	flag.Parse()

	serial, concurrent, err := parse(os.Stdin)
	if err != nil {
		log.Fatalf("parse: %v", err)
	}
	if len(serial) == 0 && len(concurrent) == 0 {
		log.Fatalf("no benchmark results found on stdin")
	}

	body, err := os.ReadFile(*readmePath)
	if err != nil {
		log.Fatalf("read readme: %v", err)
	}

	updated := string(body)
	if len(serial) > 0 {
		updated, err = splice(updated, "<!-- BENCH:SERIAL -->", "<!-- /BENCH:SERIAL -->", renderSerial(serial))
		if err != nil {
			log.Fatalf("splice serial: %v", err)
		}
	}
	if len(concurrent) > 0 {
		updated, err = splice(updated, "<!-- BENCH:CONCURRENT -->", "<!-- /BENCH:CONCURRENT -->", renderConcurrent(concurrent))
		if err != nil {
			log.Fatalf("splice concurrent: %v", err)
		}
	}

	if err := os.WriteFile(*readmePath, []byte(updated), 0o644); err != nil {
		log.Fatalf("write readme: %v", err)
	}
	fmt.Fprintf(os.Stderr, "updated %s: serial=%d, concurrent=%d\n", *readmePath, len(serial), len(concurrent))
}
