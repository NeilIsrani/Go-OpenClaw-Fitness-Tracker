# Fitness Telemetry + OpenClaw AI Investigation Agent

A Go-based fitness telemetry backend that ingests real Apple Watch health data, streams it via SSE, detects anomalies in real-time, and pairs with an OpenClaw AI agent that investigates health anomalies and sends daily WhatsApp reports.

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

## Quick Start

### Run Locally

```bash
cd src
go mod download
go run .
```

Server starts on http://localhost:8080

### Upload Apple Watch Data

1. iPhone -> Health app -> Profile picture -> Export All Health Data
2. Unzip the export -> `export.xml`
3. Upload via dashboard at http://localhost:8080 or:

```bash
curl -F "file=@export.xml" http://localhost:8080/ingest
```

### Docker

```bash
cd src
docker build -t fitness-telemetry .
docker run -p 8080:8080 fitness-telemetry
```

## API Endpoints

| Method | Path | Description | Example |
|--------|------|-------------|---------|
| POST | `/ingest` | Upload Apple Health XML | `curl -F "file=@export.xml" localhost:8080/ingest` |
| GET | `/metrics` | Query by type + time range + limit | `curl "localhost:8080/metrics?type=heart_rate&limit=50"` |
| GET | `/metrics/stats` | Aggregate stats (min, max, mean, stddev) | `curl "localhost:8080/metrics/stats?type=heart_rate"` |
| GET | `/anomalies` | List detected anomalies | `curl localhost:8080/anomalies` |
| GET | `/stream` | SSE live metric + anomaly events | `curl localhost:8080/stream` |
| GET | `/health` | Health check | `curl localhost:8080/health` |
| GET | `/` | Live dashboard | Open in browser |

### Query Parameters

**GET /metrics**
- `type` (required): `heart_rate`, `steps`, `calories`, `workout`
- `from` / `to` (optional): RFC3339 timestamps for time-range queries
- `limit` (optional): max records to return

**GET /metrics/stats**
- `type` (required): metric type
- `from` / `to` (optional): RFC3339 timestamps

**GET /anomalies**
- `type` (optional): filter by metric type

## Core Components

### store.go - Time-Series Store
In-memory store using `map[MetricType][]HealthRecord` with sorted slices. Binary search (`sort.Search`) finds the start of a time range in O(log n). `sync.RWMutex` allows concurrent readers while blocking during writes.

### parser.go - Streaming XML Parser
`xml.Decoder` reads Apple Health XML token by token, extracting heart rate, steps, calories, and workouts. Records flow through a buffered Go channel — a producer-consumer pipeline using Go concurrency primitives.

### anomaly.go - Sliding Window Z-Score Detector
Rolling window of 200 values per metric type with running sum/sumSq for O(1) mean and stddev. Warning at z >= 2.0, critical at z >= 3.0.

### sse.go - SSE Broker with Backpressure
Fan-out to multiple dashboard clients with buffered channels (size 64). Non-blocking sends drop events for slow consumers rather than blocking ingestion.

## OpenClaw AI Agent

The OpenClaw skill (`openclaw-skill/SKILL.md`) enables an LLM to:
1. Query `/anomalies` for recent anomalies
2. Get context via `/metrics/stats`
3. Correlate with workouts via `/metrics?type=workout`
4. Produce natural language health reports
5. Send daily WhatsApp summaries (morning sleep report, evening day recap)

### Setup
```bash
npx openclaw@latest
# Copy skill to ~/.openclaw/workspace/skills/fitness-investigator/
# Start the Go server
# Ask via WhatsApp: "Investigate my recent health anomalies"
```

## Deployment

Uses Terraform with ECS Fargate (reused from HW5):

```bash
cd terraform
terraform init
terraform apply
```

## Load Testing

```bash
pip install locust
locust -f locustfile.py --host=http://localhost:8080
```

Open http://localhost:8089 for the Locust dashboard.

## Project Structure

```
HW6/
|-- src/
|   |-- main.go              # Gin router, SSE handler, ingest endpoint
|   |-- models.go            # HealthRecord, AnomalyEvent, StatsResponse structs
|   |-- store.go             # In-memory time-series store (sorted slices + binary search)
|   |-- parser.go            # Streaming Apple Health XML parser
|   |-- anomaly.go           # Sliding window z-score anomaly detector
|   |-- sse.go               # SSE broker with fan-out and backpressure
|   |-- handlers.go          # REST handlers for /metrics, /metrics/stats, /anomalies, /health
|   |-- dashboard.html       # Live dashboard (Chart.js + EventSource, embedded via go:embed)
|   |-- Dockerfile           # Multi-stage build
|   |-- go.mod / go.sum
|-- openclaw-skill/
|   |-- SKILL.md             # OpenClaw fitness anomaly investigator skill
|-- terraform/               # ECS Fargate deployment (reused from HW5)
|-- locustfile.py            # Load testing
|-- PROJECT_PLAN.md          # Design document
|-- README.md
```
