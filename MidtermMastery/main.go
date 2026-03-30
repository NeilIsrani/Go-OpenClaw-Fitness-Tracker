package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
)

func main() {
	cb := NewCircuitBreaker(3, 30*time.Second)
	bh := NewBulkhead(2)

	mux := http.NewServeMux()

	// Runtime stats — watch goroutine count and heap climb during chaos
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"goroutines":    runtime.NumGoroutine(),
			"heap_alloc_mb": fmt.Sprintf("%.1f", float64(mem.HeapAlloc)/1e6),
			"heap_sys_mb":   fmt.Sprintf("%.1f", float64(mem.HeapSys)/1e6),
			"circuit":       cb.State(),
			"bulkhead_free": cap(bh.sem) - len(bh.sem),
		})
	})

	// ── CHAOS ROUTES — no protection ──────────────────────────────────────────
	//
	// /chaos/memory    allocates unbounded slices until OOM
	// /chaos/cpu       runs O(n²) z-score across N goroutines, pegs all cores
	// /chaos/goroutine spawns goroutines that block forever and never clean up

	mux.HandleFunc("/chaos/memory", handleMemoryBomb)
	mux.HandleFunc("/chaos/cpu", handleCPUBomb)
	mux.HandleFunc("/chaos/goroutine", handleGoroutineLeak)

	// ── SAFE ROUTES — each protected by a different Newman pattern ─────────────
	//
	// /safe/memory    → Fail Fast   (reject bad input before any allocation)
	// /safe/cpu       → Bulkhead    (max 2 concurrent compute jobs, rest get 503)
	// /safe/goroutine → Circuit Breaker (opens after 3 failures, auto-resets in 30s)

	mux.HandleFunc("/safe/memory", handleMemoryBombSafe)
	mux.HandleFunc("/safe/cpu", bh.Wrap(handleCPUBombSafe))
	mux.HandleFunc("/safe/goroutine", cb.Wrap(handleGoroutineLeakSafe))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║        MidtermMastery — Chaos Demo Server        ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Println("║  CHAOS  /chaos/memory  /chaos/cpu  /chaos/goroutine  ║")
	fmt.Println("║  SAFE   /safe/memory   /safe/cpu   /safe/goroutine   ║")
	fmt.Println("║  STATS  /health                                      ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Printf("Listening on :%s\n\n", port)

	log.Fatal(http.ListenAndServe(":"+port, mux))
}
