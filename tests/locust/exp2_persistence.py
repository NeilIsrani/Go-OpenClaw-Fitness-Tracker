"""
Experiment 2 — Persistence Layer Comparison: DynamoDB vs MySQL (RDS)

Goal: Measure the latency cost of adding a durable write path (DB) on top of
      the existing Redis write. Compare DynamoDB (on-demand) vs MySQL (RDS).

Requires: The Go server must expose a build-tag or env-var toggle:
    PERSIST_BACKEND=dynamo   →  writes to DynamoDB after Redis
    PERSIST_BACKEND=mysql    →  writes to MySQL after Redis
    PERSIST_BACKEND=none     →  baseline (Redis only)

Run baseline:
    PERSIST_BACKEND=none locust -f exp2_persistence.py --host http://<ALB_DNS> \
        --users 50 --spawn-rate 5 --run-time 3m --html results/exp2_baseline.html

Run DynamoDB variant:
    PERSIST_BACKEND=dynamo locust -f exp2_persistence.py ...  --html results/exp2_dynamo.html

Run MySQL variant:
    PERSIST_BACKEND=mysql  locust -f exp2_persistence.py ...  --html results/exp2_mysql.html

Compare P95/P99 ingest latency and /query latency across the three reports.

User mix:
    PersistentIngestUser  (weight=6)  — POST readings that trigger the DB write path
    HistoricalQueryUser   (weight=4)  — GET /metrics/history to exercise DB reads
"""

import random
from datetime import datetime, timezone

from locust import HttpUser, between, task


def reading_batch(metric_type: str, count: int = 5) -> dict:
    """Return a HAE-format payload with `count` readings for one metric type."""
    cfg = {
        "heart_rate": ("heart_rate",   (45, 185), "bpm"),
        "steps":      ("step_count",   (0, 300),  "count"),
        "calories":   ("active_energy",(0, 25),   "kcal"),
    }
    name, (lo, hi), unit = cfg.get(metric_type, (metric_type, (0, 100), ""))
    readings = [
        {
            "qty":  round(random.uniform(lo, hi), 2),
            "date": datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S +0000"),
        }
        for _ in range(count)
    ]
    return {
        "data": {
            "metrics": [{"name": name, "units": unit, "data": readings}],
            "workouts": [],
        }
    }


class PersistentIngestUser(HttpUser):
    """
    Drives ingest load that hits the persistence write path.
    Batches of 5 readings per request to amplify DB write pressure.
    """
    weight = 6
    wait_time = between(1, 2)

    @task(5)
    def ingest_heart_rate_batch(self):
        self.client.post(
            "/ingest/json",
            json=reading_batch("heart_rate", count=5),
            name="/ingest/json [batch:heart_rate]",
        )

    @task(3)
    def ingest_steps_batch(self):
        self.client.post(
            "/ingest/json",
            json=reading_batch("steps", count=5),
            name="/ingest/json [batch:steps]",
        )

    @task(2)
    def ingest_calories_batch(self):
        self.client.post(
            "/ingest/json",
            json=reading_batch("calories", count=5),
            name="/ingest/json [batch:calories]",
        )


class HistoricalQueryUser(HttpUser):
    """
    Exercises the read path that queries the persistent DB.
    Compares response time vs the in-memory Redis path.
    """
    weight = 4
    wait_time = between(2, 5)

    @task(4)
    def query_recent_heart_rate(self):
        # /metrics/history is the new DB-backed endpoint (to be implemented)
        # Falls back to /metrics until implemented
        self.client.get(
            "/metrics?type=heart_rate&limit=200",
            name="/metrics [history:heart_rate]",
        )

    @task(3)
    def query_recent_steps(self):
        self.client.get(
            "/metrics?type=steps&limit=200",
            name="/metrics [history:steps]",
        )

    @task(2)
    def query_stats(self):
        metric = random.choice(["heart_rate", "steps", "calories"])
        self.client.get(
            f"/metrics/stats?type={metric}",
            name="/metrics/stats [db-backed]",
        )

    @task(1)
    def query_anomalies(self):
        self.client.get("/anomalies", name="/anomalies [db-backed]")
