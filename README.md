# Go OpenClaw Fitness Tracker

A real-time fitness telemetry backend that ingests Apple Watch biometric data, runs sliding window z-score anomaly detection, and streams results live to a dashboard — with an LLM-powered investigation agent and AWS cloud deployment.

![Dashboard](dashboard.png)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         DATA SOURCES                                │
│                                                                     │
│   📱 iPhone / Apple Watch                                           │
│   Health Auto Export app  ──── POST /ingest/json ──────────────┐   │
│                                                                 │   │
│   Apple Health XML export ──── POST /ingest     ───────────┐   │   │
└─────────────────────────────────────────────────────────────│───│───┘
                                                              │   │
                        ┌─────────────────────────────────────▼───▼───┐
                        │              AWS INFRASTRUCTURE              │
                        │                                              │
                        │   ┌──────────────────────────────────────┐  │
                        │   │  Application Load Balancer (port 80) │  │
                        │   │        fitness-telemetry-alb         │  │
                        │   └──────────────────┬───────────────────┘  │
                        │                      │                       │
                        │   ┌──────────────────▼───────────────────┐  │
                        │   │      ECS Fargate (Docker container)  │  │
                        │   │       fitness-telemetry-cluster      │  │
                        │   │                                      │  │
                        │   │  ┌────────────────────────────────┐  │  │
                        │   │  │         Go / Gin Server        │  │  │
                        │   │  │           port 8080            │  │  │
                        │   │  │                                │  │  │
                        │   │  │  ┌──────────┐ ┌────────────┐  │  │  │
                        │   │  │  │ Streaming│ │  Anomaly   │  │  │  │
                        │   │  │  │  Parser  │→│  Detector  │  │  │  │
                        │   │  │  │ xml/json │ │  z-score   │  │  │  │
                        │   │  │  └────┬─────┘ └─────┬──────┘  │  │  │
                        │   │  │       │              │          │  │  │
                        │   │  │  ┌────▼──────────────▼──────┐  │  │  │
                        │   │  │  │   In-Memory Time-Series  │  │  │  │
                        │   │  │  │   Store  (RWMutex)       │  │  │  │
                        │   │  │  └──────────────┬───────────┘  │  │  │
                        │   │  │                 │               │  │  │
                        │   │  │  ┌──────────────▼───────────┐  │  │  │
                        │   │  │  │     SSE Broker           │  │  │  │
                        │   │  │  │  fan-out to dashboards   │  │  │  │
                        │   │  │  └──────────────────────────┘  │  │  │
                        │   │  └────────────────────────────────┘  │  │
                        │   └──────────────────────────────────────┘  │
                        │                                              │
                        │   ┌──────────────────────────────────────┐  │
                        │   │  ECR  Docker image registry          │  │
                        │   │  CloudWatch  container logs          │  │
                        │   └──────────────────────────────────────┘  │
                        └──────────────────────────────────────────────┘
                                           │ SSE /stream
                        ┌──────────────────▼───────────────────────────┐
                        │        Browser Dashboard  :8080               │
                        │   Heart Rate · Steps · Calories · Strain     │
                        │   Live anomaly feed · HR zone breakdown       │
                        └──────────────────────────────────────────────┘
                                           │
                        ┌──────────────────▼───────────────────────────┐
                        │         OpenClaw AI Agent                     │
                        │   Queries /anomalies + /metrics/stats         │
                        │   Correlates anomalies with workouts          │
                        │   Sends WhatsApp daily health summaries       │
                        └──────────────────────────────────────────────┘
```

---

## Tech Stack

| Layer | Tool |
|---|---|
| Backend | Go 1.24, Gin |
| Anomaly Detection | Sliding window z-score (window = 200) |
| Real-time streaming | Server-Sent Events (SSE) |
| Data store | In-memory sorted slices + `sync.RWMutex` |
| AI agent | OpenClaw + SKILL.md |
| Container | Docker (multi-stage alpine build) |
| Infrastructure | Terraform — ECS Fargate, ECR, ALB, VPC, CloudWatch |
| Load testing | Locust (Python) |
| Data sources | Apple Health XML export / Health Auto Export app (JSON) |

---

## API Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/ingest` | Upload Apple Health XML export |
| `POST` | `/ingest/json` | Receive JSON from Health Auto Export app |
| `GET` | `/stream` | SSE stream of live metrics and anomalies |
| `GET` | `/metrics?type=heart_rate&limit=50` | Query metrics by type and time range |
| `GET` | `/metrics/stats?type=heart_rate` | Min, max, mean, stddev |
| `GET` | `/anomalies` | All detected anomalies with z-scores |
| `GET` | `/strain?date=2026-02-16` | Daily strain score and HR zone breakdown |
| `GET` | `/health` | Server health check |

---

## Running Locally

**1. Start the server**
```bash
cd src
go run .
```

**2. Generate and ingest data**
```bash
# From your real Apple Health CSV export
python3 csv_to_xml_rich.py > rich_health.xml
curl -X POST http://localhost:8080/ingest -F "file=@rich_health.xml"

# Or fully synthetic
python3 gen_xml.py > synthetic_health.xml
curl -X POST http://localhost:8080/ingest -F "file=@synthetic_health.xml"
```

**3. Open dashboard**
```
http://localhost:8080
```

**4. Live data from Apple Watch (same WiFi)**

Install [Health Auto Export](https://apps.apple.com/app/health-auto-export-json-csv/id1115567033) on iPhone.
Set REST export URL to:
```
http://<your-mac-ip>:8080/ingest/json
```
Set export type to **individual samples**, include Heart Rate, Steps, Active Energy.

---

## Deploy to AWS

Prerequisites: AWS CLI configured, Docker running, Terraform installed.

```bash
cd terraform
terraform init
terraform apply
```

ALB DNS printed after apply:
```
alb_dns_name = "fitness-telemetry-alb-xxxx.us-west-2.elb.amazonaws.com"
```

Point Health Auto Export at:
```
http://fitness-telemetry-alb-xxxx.us-west-2.elb.amazonaws.com/ingest/json
```

Dashboard at the same root URL. Tear down with `terraform destroy`.

---

## Anomaly Detection

Sliding window z-score, window = 200 readings per metric type:

- **Warning** — z ≥ 2.0 (top 2.3% of distribution)
- **Critical** — z ≥ 3.0 (top 0.1% of distribution)

The OpenClaw agent cross-references `/metrics?type=workout` to flag exercise-induced spikes as expected.

---

## Daily Strain Score

0–21 scale modelled on Whoop. Time in each HR zone weighted by effort multiplier, then log-scaled.

| Zone | % Max HR | Multiplier |
|---|---|---|
| Zone 1 Light | 50–60% | 0.2× |
| Zone 2 Moderate | 60–70% | 0.5× |
| Zone 3 Vigorous | 70–80% | 1.0× |
| Zone 4 Hard | 80–90% | 2.5× |
| Zone 5 Max | 90–100% | 5.0× |
