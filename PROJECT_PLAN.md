# Fitness Telemetry + OpenClaw AI Investigation Agent

## Overview

A Go-based fitness telemetry backend that ingests real Apple Watch health data, streams it via SSE, detects anomalies in real-time, and pairs with an OpenClaw AI agent that investigates health anomalies and sends daily WhatsApp reports.

Inspired by Firetiger's stack: Go backend services, streaming ingestion pipelines, query engines, anomaly detection, ECS/Terraform deployment.

## Architecture

```
Apple Watch XML --> POST /ingest --> Go Backend (Gin, port 8080)
                                       |
                                       +-- store.go: time-series store (sorted slices + binary search + RWMutex)
                                       +-- anomaly.go: sliding window z-score detector
                                       +-- sse.go: fan-out broker with backpressure
                                       +-- REST query API (/metrics, /metrics/stats, /anomalies)
                                              ^
                                              |
                                     OpenClaw Agent (SKILL.md)
                                       +-- Queries API via curl
                                       +-- Correlates anomalies with workouts
                                       +-- Produces natural language health reports
                                       +-- Sends morning/evening summaries via WhatsApp
```

## Project Structure

```
fitness-telemetry/
|-- src/
|   |-- main.go              # Gin router, SSE handler, ingest endpoint
|   |-- models.go            # HealthRecord, AnomalyEvent, StatsResponse structs
|   |-- store.go             # In-memory time-series store (sorted slices + binary search + RWMutex)
|   |-- parser.go            # Streaming Apple Health XML parser (xml.Decoder + channel pipeline)
|   |-- anomaly.go           # Sliding window z-score anomaly detector
|   |-- sse.go               # SSE broker with fan-out and backpressure (non-blocking send)
|   |-- handlers.go          # REST handlers for /metrics, /metrics/stats, /anomalies, /health
|   |-- dashboard.html       # Single-file live dashboard (Chart.js + EventSource, embedded via go:embed)
|   |-- go.mod / go.sum
|   |-- Dockerfile           # Multi-stage build (golang:1.24-alpine -> alpine)
|-- openclaw-skill/
|   |-- SKILL.md             # OpenClaw skill: fitness anomaly investigator + WhatsApp reports
|-- terraform/               # Copied from HW5, only variable defaults change
|-- locustfile.py            # Load testing
|-- README.md
```

## API Endpoints

| Method | Path              | Description                          | Response Code |
|--------|-------------------|--------------------------------------|---------------|
| POST   | `/ingest`         | Upload Apple Health XML file         | 200           |
| GET    | `/metrics`        | Query by type + time range + limit   | 200           |
| GET    | `/metrics/stats`  | Aggregate stats (min, max, mean, stddev) | 200       |
| GET    | `/anomalies`      | List detected anomalies with severity | 200          |
| GET    | `/stream`         | SSE live metric + anomaly events     | 200 (stream)  |
| GET    | `/health`         | Health check (records, clients, uptime) | 200        |
| GET    | `/`               | HTML dashboard                       | 200           |

## Core Components Explained

### store.go - Time-Series Store
Unlike HW5's `map[int]Product` (lookup by ID), health data needs time-range queries ("all heart rate readings between 3pm and 5pm"). Uses `map[MetricType][]HealthRecord` where each slice is sorted by timestamp. Binary search finds the start of a time range in O(log n), then scans forward. `sync.RWMutex` allows concurrent readers (multiple dashboard clients querying stats) while blocking during writes (ingestion).

### parser.go - Streaming XML Parser
Apple Health exports can be 1GB+. Rather than loading the entire file, `xml.Decoder` reads token by token, extracting only the metric types we care about (heart rate, steps, calories, workouts) and ignoring hundreds of others. Records flow into a buffered Go channel as they're parsed — the main goroutine consumes them for storage, broadcasting, and anomaly detection. This is a real producer-consumer pipeline using Go concurrency primitives.

### anomaly.go - Sliding Window Z-Score Detector
Maintains a rolling window of recent values per metric type (e.g., last 200 heart rate readings). Tracks running `sum` and `sumSq` for O(1) mean and standard deviation computation. When a new value arrives, calculates z-score = `|value - mean| / stddev`. Warning at z >= 2.0 (top 2.3% of distribution), critical at z >= 3.0 (top 0.1%). Example: if average HR is 72 with stddev 10, a reading of 155 bpm would be z-score ~8.3 = critical anomaly.

### sse.go - SSE Broker with Backpressure
Manages fan-out to multiple connected dashboard clients. Each client gets a buffered channel (size 64). On broadcast, the broker does a non-blocking send to each channel — if a client's buffer is full (slow browser), that event is dropped rather than blocking the entire ingestion pipeline. This is how real observability streaming systems handle slow consumers without blocking producers.

### Go Concurrency Patterns Used
- `sync.RWMutex` on the store: concurrent readers, exclusive writers
- Goroutine + channel pipeline: parser goroutine -> buffered chan -> main loop (store + broadcast + detect)
- SSE broker: goroutine per client, non-blocking fan-out
- These patterns directly mirror how high-scale telemetry systems handle concurrent ingestion and querying

## OpenClaw AI Investigation Agent

### What It Does
The OpenClaw agent acts as an LLM-powered health analyst. When triggered (manually or on schedule), it:
1. Calls `GET /anomalies` to find recent anomalies
2. For each anomaly, queries `/metrics/stats` for context (average HR that day)
3. Queries `/metrics?type=workout` to check if a workout correlates with the spike
4. Uses the LLM to produce a natural language report explaining what happened, why, and whether it's concerning

### Example Investigation Output
> "On Feb 15 at 3:47 PM, your heart rate spiked to 182 bpm (z-score: 3.4, critical). This correlates with a 45-minute running workout that started at 3:30 PM where you burned 420 kcal. Your average resting heart rate that day was 68 bpm. This spike appears to be exercise-induced and within expected ranges for high-intensity cardio. No action needed."

### WhatsApp Daily Reports
OpenClaw has native WhatsApp support via Baileys. Scheduled reports:

**Morning (sleep summary):**
> "Good morning! Sleep summary: resting HR averaged 56 bpm overnight (normal). 1 minor anomaly at 3:12 AM (HR dipped to 42, z-score 2.1 -- likely deep sleep). No concerns."

**Evening (day recap):**
> "Day recap: 11,200 steps, 380 active calories, 1 workout (38min run, 290 kcal). Heart rate peaked at 172 during your run (expected). Resting HR today: 62 bpm. All good!"

### SKILL.md Structure
The OpenClaw skill is a single `SKILL.md` file with:
- YAML frontmatter (name, description)
- Instructions telling the LLM which API endpoints to call
- Investigation process (fetch anomalies -> get context -> correlate with workouts -> report)
- Output format guidelines

### Setup
1. Install OpenClaw: `npx openclaw@latest`
2. Set up WhatsApp: follow OpenClaw WhatsApp setup (scan QR code)
3. Copy skill to `~/.openclaw/workspace/skills/fitness-investigator/`
4. Start the Go server
5. Ask via WhatsApp: "Investigate my recent health anomalies"
6. Set up cron for automatic morning/evening reports

## Deployment
Reuses HW5 Terraform infrastructure (ECS Fargate, ECR, CloudWatch, VPC). Only variable defaults change:
- `ecr_repository_name` -> "fitness-telemetry"
- `service_name` -> "fitness-telemetry"

Dockerfile is identical to HW5 (multi-stage build). `dashboard.html` is compiled into the binary via `go:embed`.

## How to Get Apple Watch Data
1. iPhone -> Health app -> Profile picture -> Export All Health Data
2. Unzip the export -> `export.xml`
3. Upload via dashboard or `curl -F "file=@export.xml" http://localhost:8080/ingest`

## Implementation Order

### Day 1: Core pipeline
1. Initialize Go module with Gin dependency
2. `models.go` -- struct definitions
3. `store.go` -- sorted-slice store with binary search queries and stats
4. `parser.go` -- streaming XML parser with channel output

### Day 2: Streaming + API
5. `sse.go` -- broker with fan-out and backpressure
6. `anomaly.go` -- sliding window z-score detector
7. `handlers.go` -- REST handlers for /metrics, /metrics/stats, /anomalies, /health
8. `main.go` -- Gin router, /ingest pipeline, /stream SSE, / dashboard
9. `dashboard.html` -- Chart.js charts + EventSource + file upload

### Day 3: AI agent + deploy
10. `openclaw-skill/SKILL.md` -- investigation skill with WhatsApp report instructions
11. `Dockerfile` -- multi-stage build
12. Copy `terraform/` from HW5, update variable defaults
13. `locustfile.py` -- load test queries and SSE
14. `README.md` -- architecture, screenshots, curl examples
15. Test end-to-end: ingest Apple Watch data -> dashboard updates -> OpenClaw investigates -> WhatsApp report

## Resume Bullet (aligned to Grace's Firetiger bio)

```
Built Go backend for an AI-powered health investigation agent that analyzes Apple Watch telemetry and detects anomalies.
- Designed streaming ingestion pipeline with goroutine/channel concurrency; exposed time-range query API with binary search.
- Built OpenClaw AI agent skill that correlates anomalies with workout data and sends daily WhatsApp health reports.
- Deployed on ECS Fargate via Terraform; real-time z-score anomaly detection with SSE streaming to live dashboard.
```
