// run_tests.go — Experiment 2 performance test
//
// Usage:
//   go run tests/run_tests.go -url http://YOUR-ALB -backend none   -out results/exp2_baseline.json
//   go run tests/run_tests.go -url http://YOUR-ALB -backend dynamo -out results/exp2_dynamo.json
//   go run tests/run_tests.go -url http://YOUR-ALB -backend mysql  -out results/exp2_mysql.json
//
// Runs 150 operations: 50 ingest, 50 query, 50 stats.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"
)

type TestResult struct {
	Operation    string  `json:"operation"`
	ResponseTime float64 `json:"response_time"` // milliseconds
	Success      bool    `json:"success"`
	StatusCode   int     `json:"status_code"`
	Backend      string  `json:"backend"`
	Timestamp    string  `json:"timestamp"`
}

var metrics = []string{"heart_rate", "step_count", "active_energy"}
var units = map[string]string{
	"heart_rate":    "bpm",
	"step_count":    "count",
	"active_energy": "kcal",
}
var ranges = map[string][2]float64{
	"heart_rate":    {55, 180},
	"step_count":    {0, 300},
	"active_energy": {0, 25},
}

func haePayload(metricName string, count int) map[string]interface{} {
	lo, hi := ranges[metricName][0], ranges[metricName][1]
	readings := make([]map[string]interface{}, count)
	for i := range readings {
		readings[i] = map[string]interface{}{
			"qty":  lo + rand.Float64()*(hi-lo),
			"date": time.Now().UTC().Format("2006-01-02 15:04:05 +0000"),
		}
	}
	return map[string]interface{}{
		"data": map[string]interface{}{
			"metrics": []map[string]interface{}{
				{"name": metricName, "units": units[metricName], "data": readings},
			},
			"workouts": []interface{}{},
		},
	}
}

func main() {
	baseURL := flag.String("url", "http://localhost:8080", "Base URL of the fitness telemetry API")
	backend := flag.String("backend", "none", "Persistence backend: none | dynamo | mysql")
	outFile := flag.String("out", "test_results.json", "Output JSON file path")
	flag.Parse()

	fmt.Printf("Target:  %s\n", *baseURL)
	fmt.Printf("Backend: %s\n", *backend)
	fmt.Printf("Output:  %s\n", *outFile)
	fmt.Printf("Running 150 operations (50 ingest + 50 query + 50 stats)...\n\n")

	client := &http.Client{Timeout: 10 * time.Second}
	results := make([]TestResult, 0, 150)

	record := func(op string, start time.Time, resp *http.Response, err error, successCode int) {
		elapsed := float64(time.Since(start).Milliseconds())
		r := TestResult{
			Operation:    op,
			ResponseTime: elapsed,
			Backend:      *backend,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
		}
		if err != nil {
			r.Success = false
			r.StatusCode = 0
		} else {
			r.StatusCode = resp.StatusCode
			r.Success = resp.StatusCode == successCode
			resp.Body.Close()
		}
		results = append(results, r)
	}

	// ── Phase 1: 50 × ingest (batches of 5 readings) ──────────────────────────
	fmt.Println("Phase 1: Ingesting 50 batches...")
	for i := 0; i < 50; i++ {
		metric := metrics[i%3]
		payload, _ := json.Marshal(haePayload(metric, 5))

		start := time.Now()
		resp, err := client.Post(*baseURL+"/ingest/json", "application/json",
			bytes.NewBuffer(payload))
		elapsed := float64(time.Since(start).Milliseconds())

		r := TestResult{
			Operation:    "ingest",
			ResponseTime: elapsed,
			Backend:      *backend,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
		}
		if err != nil {
			r.Success = false
			fmt.Printf("  [%02d] ERROR: %v\n", i+1, err)
		} else {
			r.StatusCode = resp.StatusCode
			r.Success = resp.StatusCode == 200
			resp.Body.Close()
			fmt.Printf("  [%02d] %d  %s  %.1fms\n", i+1, resp.StatusCode, metric, elapsed)
		}
		results = append(results, r)
	}

	// ── Phase 2: 50 × query /metrics ──────────────────────────────────────────
	fmt.Println("\nPhase 2: Querying metrics (50 reads)...")
	queryMetrics := []string{"heart_rate", "steps", "calories"}
	for i := 0; i < 50; i++ {
		m := queryMetrics[i%3]
		url := fmt.Sprintf("%s/metrics?type=%s&limit=200", *baseURL, m)

		start := time.Now()
		resp, err := client.Get(url)
		record("query", start, resp, err, 200)

		elapsed := float64(time.Since(start).Milliseconds())
		if err != nil {
			fmt.Printf("  [%02d] ERROR: %v\n", i+1, err)
		} else {
			fmt.Printf("  [%02d] %d  %s  %.1fms\n", i+1, resp.StatusCode, m, elapsed)
		}
	}

	// ── Phase 3: 50 × /metrics/stats ──────────────────────────────────────────
	fmt.Println("\nPhase 3: Stats queries (50 reads)...")
	for i := 0; i < 50; i++ {
		m := queryMetrics[i%3]
		url := fmt.Sprintf("%s/metrics/stats?type=%s", *baseURL, m)

		start := time.Now()
		resp, err := client.Get(url)
		record("stats", start, resp, err, 200)

		elapsed := float64(time.Since(start).Milliseconds())
		if err != nil {
			fmt.Printf("  [%02d] ERROR: %v\n", i+1, err)
		} else {
			fmt.Printf("  [%02d] %d  %s  %.1fms\n", i+1, resp.StatusCode, m, elapsed)
		}
	}

	// ── Write output ───────────────────────────────────────────────────────────
	if err := os.MkdirAll("results", 0755); err != nil {
		fmt.Printf("Cannot create results dir: %v\n", err)
		os.Exit(1)
	}
	out, err := os.Create(*outFile)
	if err != nil {
		fmt.Printf("Cannot create output file: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.Encode(results)

	// ── Summary ────────────────────────────────────────────────────────────────
	var totalSuccess int
	var totalTime float64
	opTimes := map[string][]float64{}
	for _, r := range results {
		if r.Success {
			totalSuccess++
		}
		totalTime += r.ResponseTime
		opTimes[r.Operation] = append(opTimes[r.Operation], r.ResponseTime)
	}

	fmt.Printf("\n═══ Summary (%s backend) ═══\n", *backend)
	fmt.Printf("Total:   %d operations\n", len(results))
	fmt.Printf("Success: %d (%.1f%%)\n", totalSuccess, float64(totalSuccess)/float64(len(results))*100)
	fmt.Printf("Avg:     %.1f ms\n", totalTime/float64(len(results)))
	for _, op := range []string{"ingest", "query", "stats"} {
		times := opTimes[op]
		if len(times) == 0 {
			continue
		}
		var sum float64
		for _, t := range times {
			sum += t
		}
		fmt.Printf("  %-8s avg %.1f ms\n", op, sum/float64(len(times)))
	}
	fmt.Printf("Output:  %s\n", *outFile)
}
