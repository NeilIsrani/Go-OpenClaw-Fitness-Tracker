// run_tests.go — Experiment 2 performance test
//
// Usage:
//   go run tests/run_tests.go -url http://YOUR-ALB -backend none   -out results/exp2_baseline.json
//   go run tests/run_tests.go -url http://YOUR-ALB -backend dynamo -out results/exp2_dynamo.json
//   go run tests/run_tests.go -url http://YOUR-ALB -backend mysql  -out results/exp2_mysql.json
//
// Runs 250 operations split across four phases:
//   Phase 1 — 50 concurrent ingest requests (tests RWMutex write-lock contention)
//   Phase 2 — 50 sequential metric queries
//   Phase 3 — pre-seed 10,000 records to stress the O(n) stats scan
//   Phase 4 — 50 stats aggregations against the seeded store

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
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

// percentile returns the p-th percentile of a pre-sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func main() {
	baseURL := flag.String("url", "http://localhost:8080", "Base URL of the fitness telemetry API")
	backend := flag.String("backend", "none", "Persistence backend: none | dynamo | mysql")
	outFile := flag.String("out", "test_results.json", "Output JSON file path")
	flag.Parse()

	fmt.Printf("Target:  %s\n", *baseURL)
	fmt.Printf("Backend: %s\n", *backend)
	fmt.Printf("Output:  %s\n", *outFile)
	fmt.Printf("Running 250 operations (50 concurrent ingest + 50 query + seed + 50 stats)...\n\n")

	client := &http.Client{Timeout: 15 * time.Second}

	var (
		results []TestResult
		mu      sync.Mutex
	)

	appendResult := func(r TestResult) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
	}

	queryMetrics := []string{"heart_rate", "steps", "calories"}

	// ── Phase 1: 50 concurrent ingest requests ────────────────────────────────
	// Concurrent sends expose sync.RWMutex write-lock contention — the key
	// difference from the original sequential design.
	fmt.Println("Phase 1: 50 concurrent ingest requests...")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
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
				r.Success = resp.StatusCode == 202 // ingest now returns 202 Accepted
				resp.Body.Close()
				fmt.Printf("  [%02d] %d  %s  %.1fms\n", i+1, resp.StatusCode, metric, elapsed)
			}
			appendResult(r)
		}(i)
	}
	wg.Wait()
	fmt.Println("  Phase 1 done — all 50 goroutines returned")

	// ── Phase 2: 50 sequential metric queries ─────────────────────────────────
	fmt.Println("\nPhase 2: Querying metrics (50 sequential reads)...")
	for i := 0; i < 50; i++ {
		m := queryMetrics[i%3]
		url := fmt.Sprintf("%s/metrics?type=%s&limit=200", *baseURL, m)

		start := time.Now()
		resp, err := client.Get(url)
		elapsed := float64(time.Since(start).Milliseconds())

		r := TestResult{
			Operation:    "query",
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
			fmt.Printf("  [%02d] %d  %s  %.1fms\n", i+1, resp.StatusCode, m, elapsed)
		}
		results = append(results, r)
	}

	// ── Phase 3: pre-seed 10,000 records ──────────────────────────────────────
	// Sends 2,000 batches of 5 readings each via a capped goroutine pool (50
	// concurrent). This grows the in-memory store to ~10,000 records so the
	// subsequent stats phase exercises the O(n) scan at realistic data volumes.
	fmt.Println("\nPhase 3: Pre-seeding 10,000 records (2,000 batches × 5 readings)...")
	seedStart := time.Now()
	sem := make(chan struct{}, 50) // cap at 50 concurrent seed requests
	var seedWg sync.WaitGroup
	for i := 0; i < 2000; i++ {
		seedWg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer seedWg.Done()
			defer func() { <-sem }()
			metric := metrics[i%3]
			payload, _ := json.Marshal(haePayload(metric, 5))
			resp, err := client.Post(*baseURL+"/ingest/json", "application/json",
				bytes.NewBuffer(payload))
			if err == nil {
				resp.Body.Close()
			}
		}(i)
		if (i+1)%500 == 0 {
			fmt.Printf("  seeded %d / 2000 batches\n", i+1)
		}
	}
	seedWg.Wait()

	// Give the worker pool time to drain the SQS / channel queue before querying
	fmt.Printf("  seeding complete in %.1fs — waiting 5s for workers to drain...\n",
		time.Since(seedStart).Seconds())
	time.Sleep(5 * time.Second)

	// ── Phase 4: 50 stats aggregations against seeded store ───────────────────
	// Stats() is O(n) in accumulated records. At 10,000 records this scan is
	// measurably more expensive than the 250-record baseline from the original test.
	fmt.Println("\nPhase 4: Stats queries against seeded store (50 reads)...")
	for i := 0; i < 50; i++ {
		m := queryMetrics[i%3]
		url := fmt.Sprintf("%s/metrics/stats?type=%s", *baseURL, m)

		start := time.Now()
		resp, err := client.Get(url)
		elapsed := float64(time.Since(start).Milliseconds())

		r := TestResult{
			Operation:    "stats",
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
			fmt.Printf("  [%02d] %d  %s  %.1fms\n", i+1, resp.StatusCode, m, elapsed)
		}
		results = append(results, r)
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

	// ── Summary with percentiles ───────────────────────────────────────────────
	var totalSuccess int
	opTimes := map[string][]float64{}
	for _, r := range results {
		if r.Success {
			totalSuccess++
		}
		opTimes[r.Operation] = append(opTimes[r.Operation], r.ResponseTime)
	}

	fmt.Printf("\n═══ Summary (%s backend) ═══\n", *backend)
	fmt.Printf("Total:   %d operations\n", len(results))
	fmt.Printf("Success: %d (%.1f%%)\n", totalSuccess, float64(totalSuccess)/float64(len(results))*100)
	fmt.Printf("\n  %-8s  %7s  %7s  %7s  %7s  %7s\n", "op", "avg", "P50", "P95", "P99", "max")
	fmt.Printf("  %s\n", "─────────────────────────────────────────────────────")
	for _, op := range []string{"ingest", "query", "stats"} {
		times := opTimes[op]
		if len(times) == 0 {
			continue
		}
		sort.Float64s(times)
		var sum float64
		for _, t := range times {
			sum += t
		}
		fmt.Printf("  %-8s  %6.1f  %6.1f  %6.1f  %6.1f  %6.1f  ms\n",
			op,
			sum/float64(len(times)),
			percentile(times, 50),
			percentile(times, 95),
			percentile(times, 99),
			times[len(times)-1],
		)
	}
	fmt.Printf("\nOutput:  %s\n", *outFile)
}
