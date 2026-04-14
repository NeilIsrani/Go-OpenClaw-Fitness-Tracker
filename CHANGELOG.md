# Changelog

All notable changes to the fitness telemetry platform are documented here.

---

## [Unreleased] — 2026-04-12

### Added
- **Async ingest pipeline via SQS** (`src/queue.go`)
  - `IngestQueue` interface with two implementations: `SQSQueue` (AWS SQS, enabled via `SQS_QUEUE_URL` env var) and `ChannelQueue` (in-process buffered channel, local fallback)
  - `SQSQueue` uses long-polling (`WaitTimeSeconds=20`) to minimise empty receive calls; deletes messages from the queue after forwarding to prevent redelivery
  - `ChannelQueue` is a 1024-slot buffered channel with identical semantics — no code change needed to switch between backends
  - Worker pool of 10 goroutines drains the queue; each worker runs `Store.Add` → `Detector.Detect` → `Broker.Broadcast` in sequence
  - `NewQueue` factory selects the backend automatically at startup based on environment

- **SSE bulkhead** (`src/main.go`)
  - `/stream` handler now rejects connections beyond 500 concurrent clients with `HTTP 503` and a JSON body containing `limit` and `current` counts
  - Eliminates the ALB idle-timeout failures observed in Experiment 3 (Test 3 recorded 144 failures caused by the ALB cutting connections after 60 seconds — bulkhead fast-fails instead of hanging)

- **`Queue` field on `App` struct** (`src/handlers.go`)
  - Ingest queue is now a first-class dependency alongside `Store`, `Broker`, and `Detector`

### Changed
- **`POST /ingest/json`** now returns `202 Accepted` instead of `200 OK`
  - Response body changed from `{"message":"ok","records":N,"anomalies":N}` to `{"status":"queued","queued":N,"dropped":N}`
  - All processing (store write, anomaly detection, SSE broadcast) moved out of the request goroutine into background workers
  - Ingest handler latency is now: JSON parse + queue write only — no longer blocked by SSE fan-out across all connected clients

- **`POST /ingest`** (XML upload) updated to match — records are enqueued instead of processed synchronously; also returns `202 Accepted`

- **`go.mod` / `go.sum`** — added AWS SDK v2 dependencies:
  - `github.com/aws/aws-sdk-go-v2 v1.41.5`
  - `github.com/aws/aws-sdk-go-v2/config v1.32.14`
  - `github.com/aws/aws-sdk-go-v2/service/sqs v1.42.25`
  - `github.com/aws/smithy-go v1.24.2`

### Removed
- **`openclaw-skill/`** directory — AI agent prompt file removed; no Go code was coupled to it
- Synchronous `Store.Add` / `Broker.Broadcast` / `Detector.Detect` calls from ingest handlers — all processing is now exclusively in worker goroutines

---

## [0.4.0] — 2026-03-30 (`10e8c24`)

### Added
- Three load test experiments (`tests/locust/`, `tests/run_tests.go`)
  - **Experiment 1** (`exp1_scaling.py`) — horizontal scaling: 90 users, 83 req/s, ECS task count 1→2→4; measures P95/P99 ingest latency vs task count
  - **Experiment 2** (`run_tests.go`) — in-memory baseline: 150 sequential operations (50 ingest, 50 query, 50 stats) against the live ALB; establishes the performance ceiling before any persistence layer
  - **Experiment 3** (`exp3_sse_fanout.py`) — SSE fan-out ceiling: 550 concurrent users (495 SSE clients + 55 anomaly drivers), chaos injection by halving ECS task count mid-test

---

## [0.3.0] — 2026-02-23 (`14fe052`)

### Added
- Application Load Balancer in Terraform (`terraform/modules/`) — ALB with HTTPS listener routes traffic across ECS tasks
- Dashboard UI refresh (`src/dashboard.html`) — live charts for heart rate, steps, calories, anomaly feed

---

## [0.2.0] — 2026-02-23 (`6cd6716`, `e8f1245`)

### Added
- XML ingest endpoint (`POST /ingest`) — streams Apple Health export XML through `io.Pipe` into parser; no full-file buffer required
- Exercise / workout data support — `Workout` metric type with `WorkoutType` and `Duration` fields on `HealthRecord`
- Strain score endpoint (`GET /strain`) — Whoop-style 0–21 score derived from heart rate zone time distribution
- Health Auto Export JSON format support (`POST /ingest/json`) — accepts the HAE envelope directly from the iOS app

---

## [0.1.0] — 2026-02-18 — 2026-02-21 (`cd1bebc`, `a5cba8e`, `a1c3368`)

### Added
- Go/Gin HTTP server (`src/main.go`) with routes: `/ingest`, `/ingest/json`, `/metrics`, `/metrics/stats`, `/metrics/all`, `/anomalies`, `/strain`, `/health`, `/stream`
- In-memory time-series store (`src/store.go`) — sorted slices per metric type with binary-search insert (`O(log n)`) and range query; protected by `sync.RWMutex`
- Sliding-window z-score anomaly detector (`src/anomaly.go`) — 200-sample window with O(1) running `sum`/`sumSq` accumulator; warning threshold z≥2.0, critical z≥3.0; minimum 10-sample warm-up before detection activates
- SSE broker (`src/sse.go`) — goroutine-per-client fan-out with 64-slot per-client buffer; non-blocking `select/default` on broadcast drops slow clients rather than blocking the write path
- Data models (`src/models.go`) — `HealthRecord`, `AnomalyEvent`, `StatsResponse`, `StrainScore`, `HealthCheckResponse`
- Terraform infrastructure (`terraform/`) — ECS Fargate cluster, ECR repository, VPC with dual public subnets, CloudWatch log group, IAM task role; all parameterised via `variables.tf`
- Dockerfile — multi-stage build (`golang:1.24-alpine` builder, `alpine` runtime); embeds `dashboard.html` at compile time via `//go:embed`
- OpenClaw AI agent integration — Claude Code skill (`openclaw-skill/SKILL.md`) for anomaly investigation and WhatsApp health reports *(removed in Unreleased)*
