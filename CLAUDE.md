# CLAUDE.md — Real-Time Health Telemetry Platform
### Living Project Plan · Last updated 2026-03-29

Read this before touching anything.

---

## Current Architecture

```
Apple Watch / iOS
      ↓ POST /ingest/json  or  POST /ingest (XML)
  ALB (HTTPS)
      ↓
 ECS Fargate (Go/Gin)  ←→  In-memory store (sync.RWMutex)
      ↓                          ↓ z-score on every write
  SSE Broker ──────── push ──── Anomaly Detector
      ↓
  Browser Dashboard
```

**File map**
- Entry point: `src/main.go` → routes → `src/handlers.go`
- Rolling window state: `src/store.go` — in-memory slice per metric type, 200-sample window
- Anomaly detection: `src/anomaly.go` — z-score, runs on every ingest
- SSE fan-out: goroutine-per-client in `src/handlers.go`, no external broker
- Infrastructure: `terraform/` — ECS Fargate, ECR, ALB, VPC, CloudWatch

---

## Architecture Evolution (open — not yet implementing)

**Option A — Kafka**
```
Ingest → Kafka: raw-biometrics (partition by user_id)
             ↓
     Anomaly Consumer Service → Kafka: anomaly-events
             ↓
     SSE Broker (consumer group)
```
Client: `sarama`. Topics: `raw-biometrics`, `anomaly-events`.

**Option B — SNS + SQS + Lambda** (team has prior experience here)
```
Ingest → SNS → SQS → Anomaly Lambda → SQS → SSE Broker
                   → Persistence Lambda → DynamoDB / RDS
```

**Decision:** open. Do not begin either migration without explicit instruction.
Redis skepticism noted — target is Redis out of the hot path entirely either way.

---

## Constraints — Do Not Touch Without Instruction

- `src/handlers.go` ingest handlers
- `terraform/` — no infra changes until persistence backend is chosen
- SSE `/stream` endpoint contract — event schema must stay identical
- Do not remove Redis until a replacement is confirmed working

---

## Project Plan

### Phase 0 — Already done
- [x] Go/Gin ingest server with XML + JSON support
- [x] Sliding window z-score anomaly detector
- [x] SSE broker with fan-out
- [x] ECS Fargate + ALB + Terraform deploy
- [x] Basic Locust read-load file (`locustfile.py`)
- [x] Strain score (0–21 Whoop-style)
- [x] OpenClaw AI agent (anomaly investigation + WhatsApp summary)

---

### Phase 1 — Experiments (implement now)

#### Experiment 1 — Horizontal Scaling: Ingest Throughput vs ECS Task Count
**File:** `tests/locust/exp1_scaling.py`
**Status:** locust file written — ready to run

Steps:
1. Deploy current build to ECS with `desired_count = 1`
2. Run: `locust -f tests/locust/exp1_scaling.py --host http://<ALB> --users 90 --spawn-rate 10 --run-time 5m --html results/exp1_t1.html`
3. Change `desired_count` → 2, 4, 8 in Terraform, re-run each time
4. Compare P95/P99 ingest latency, ECS CPU, anomaly detection latency across reports

**Measure:** P95/P99 `/ingest/json` latency · ECS CPU per task · `/health` goroutine count

---

#### Experiment 2 — Persistence Layer: DynamoDB vs MySQL (RDS)
**File:** `tests/locust/exp2_persistence.py`
**Status:** locust file written — Go server changes needed first

Steps:
1. **Add `PERSIST_BACKEND` env var to the Go server** — when set, dual-write each ingest to the chosen DB after the in-memory write
2. Add DynamoDB table in Terraform: partition key `user_id`, sort key `timestamp`, on-demand billing
3. Add RDS MySQL instance in Terraform (t3.micro, single-AZ)
4. Run three variants with identical Locust load:
   - `PERSIST_BACKEND=none`  → baseline
   - `PERSIST_BACKEND=dynamo`
   - `PERSIST_BACKEND=mysql`
5. Compare: write latency overhead · read latency for `/metrics?limit=200` · cost/million writes

**Go changes needed:**
- `src/persist/dynamo.go` — `Write(record HealthRecord) error`
- `src/persist/mysql.go`  — same interface
- Toggle called from `handlers.go` after existing in-memory write

---

#### Experiment 3 — SSE Fan-Out: Real-Time Broadcast Ceiling
**File:** `tests/locust/exp3_sse_fanout.py`
**Status:** locust file written — ready to run

Steps:
1. Run: `locust -f tests/locust/exp3_sse_fanout.py --host http://<ALB> --users 550 --spawn-rate 5 --run-time 10m --html results/exp3.html`
2. At the 5-minute mark, kill 50% of ECS tasks via CLI:
   ```bash
   aws ecs update-service --cluster fitness-telemetry-cluster \
       --service fitness-telemetry-service --desired-count <half>
   ```
3. Observe reconnection storm, failure rate spike, recovery time in Locust chart

**Measure:** SSE connection hold time · end-to-end anomaly delivery latency · ECS memory per task · ALB active connections (CloudWatch)

---

### Phase 2 — Persistence Backend (after Exp 2 results)
Implement `src/persist/` package. Interface:
```go
type Backend interface {
    Write(ctx context.Context, r HealthRecord) error
    Query(ctx context.Context, metricType string, limit int) ([]HealthRecord, error)
}
```
Wired to ingest handler via env var. Pick winning backend from Exp 2 data.

---

### Phase 3 — Streaming Backbone Migration (after persistence is stable)
Execute Kafka or SNS/SQS migration in slices (see architecture section above).
Do not start until Phase 2 is complete and Exp 1–3 results are written up.

---

## Running Experiments on AWS

All experiments (1 and 3) run against the live ALB. Get the DNS first:

```bash
cd terraform
terraform output alb_dns_name
# → fitness-telemetry-alb-xxxx.us-west-2.elb.amazonaws.com
export ALB="http://$(terraform output -raw alb_dns_name)"
```

Make a results directory:
```bash
mkdir -p results
```

### Experiment 1 — scale ECS tasks via Terraform, run Locust each time

```bash
# t=1 (default)
locust -f tests/locust/exp1_scaling.py --host $ALB \
       --users 90 --spawn-rate 10 --run-time 5m --headless \
       --html results/exp1_t1.html --csv results/exp1_t1

# t=2
cd terraform && terraform apply -var="ecs_count=2" -auto-approve && cd ..
locust -f tests/locust/exp1_scaling.py --host $ALB \
       --users 90 --spawn-rate 10 --run-time 5m --headless \
       --html results/exp1_t2.html --csv results/exp1_t2

# t=4
cd terraform && terraform apply -var="ecs_count=4" -auto-approve && cd ..
locust -f tests/locust/exp1_scaling.py --host $ALB \
       --users 90 --spawn-rate 10 --run-time 5m --headless \
       --html results/exp1_t4.html --csv results/exp1_t4

# t=8
cd terraform && terraform apply -var="ecs_count=8" -auto-approve && cd ..
locust -f tests/locust/exp1_scaling.py --host $ALB \
       --users 90 --spawn-rate 10 --run-time 5m --headless \
       --html results/exp1_t8.html --csv results/exp1_t8

# Reset to 1 task after experiment
cd terraform && terraform apply -var="ecs_count=1" -auto-approve
```

### Experiment 2 — Persistence Layer (local dev + AWS)

Prototype the `PERSIST_BACKEND` toggle locally first:
```bash
cd src && PERSIST_BACKEND=none go run .
# once working, build + push to ECR, then run against ALB
locust -f tests/locust/exp2_persistence.py --host $ALB \
       --users 50 --spawn-rate 5 --run-time 3m --headless \
       --html results/exp2_dynamo.html --csv results/exp2_dynamo
```

### Experiment 3 — SSE fan-out on AWS (must be AWS — SSE hold time needs real network)

```bash
locust -f tests/locust/exp3_sse_fanout.py --host $ALB \
       --users 550 --spawn-rate 5 --run-time 10m --headless \
       --html results/exp3.html --csv results/exp3

# At the 5-minute mark, inject chaos:
aws ecs update-service \
    --cluster fitness-telemetry-cluster \
    --service fitness-telemetry-service \
    --desired-count 1   # halve the current count
```

---

## Open Decisions

| Decision | Options | Status |
|---|---|---|
| Streaming backbone | Kafka (sarama) vs SNS+SQS+Lambda | Open |
| Persistence backend | DynamoDB vs MySQL (RDS) | Experiment 2 decides this |
| Locust results storage | Local HTML vs S3 | Open |
| Redis removal timeline | After Phase 3 | Planned |
