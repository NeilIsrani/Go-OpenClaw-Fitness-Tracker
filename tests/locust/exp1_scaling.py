"""
Experiment 1 — Horizontal Scaling: Ingest Throughput vs ECS Task Count

Goal: Hold ingest load at ~5,000 readings/minute while scaling ECS tasks 1→2→4→8.
      Identify whether the bottleneck is compute (ECS), network (ALB), or state (Redis).

Run:
    locust -f exp1_scaling.py --host http://<ALB_DNS> \
           --users 90 --spawn-rate 10 --run-time 5m \
           --html results/exp1_t1.html

Repeat with ECS task count = 1, 2, 4, 8. Compare HTML reports.

User mix:
    IngestUser  (weight=7)  — POST synthetic readings  → drives ~83 req/s at 90 users
    ReadUser    (weight=3)  — GET metrics/stats/anomalies → background read load
"""

import json
import random
import time
from datetime import datetime, timezone

from locust import HttpUser, between, events, tag, task


def hae_payload(metric_name: str, qty: float, unit: str) -> dict:
    """Wrap a single reading in the Health Auto Export envelope the server expects."""
    date_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S +0000")
    return {
        "data": {
            "metrics": [
                {
                    "name": metric_name,
                    "units": unit,
                    "data": [{"qty": qty, "date": date_str}],
                }
            ],
            "workouts": [],
        }
    }


def synthetic_reading(metric_type: str) -> dict:
    """Generate a single HAE-format payload for the given metric."""
    cfg = {
        "heart_rate": ("heart_rate", (45, 185), "bpm"),
        "steps":      ("step_count", (0, 300),  "count"),
        "calories":   ("active_energy", (0, 25), "kcal"),
    }
    name, (lo, hi), unit = cfg.get(metric_type, (metric_type, (0, 100), ""))
    return hae_payload(name, round(random.uniform(lo, hi), 2), unit)


class IngestUser(HttpUser):
    """
    Drives write load. Target: ~83 req/s across the user pool to hit 5,000/min.
    wait_time of 1s with 90 users ≈ 90 req/s before latency effects.
    """
    weight = 7
    wait_time = between(0.8, 1.2)

    @task(6)
    def ingest_heart_rate(self):
        self.client.post(
            "/ingest/json",
            json=synthetic_reading("heart_rate"),
            name="/ingest/json [heart_rate]",
        )

    @task(2)
    def ingest_steps(self):
        self.client.post(
            "/ingest/json",
            json=synthetic_reading("steps"),
            name="/ingest/json [steps]",
        )

    @task(2)
    def ingest_calories(self):
        self.client.post(
            "/ingest/json",
            json=synthetic_reading("calories"),
            name="/ingest/json [calories]",
        )

    @task(1)
    def health_check(self):
        self.client.get("/health", name="/health")


class ReadUser(HttpUser):
    """Background read traffic — simulates dashboard polling."""
    weight = 3
    wait_time = between(2, 4)

    @task(4)
    def read_hr_stats(self):
        self.client.get("/metrics/stats?type=heart_rate",
                        name="/metrics/stats [heart_rate]")

    @task(3)
    def read_anomalies(self):
        self.client.get("/anomalies", name="/anomalies")

    @task(2)
    def read_metrics(self):
        metric = random.choice(["heart_rate", "steps", "calories"])
        self.client.get(f"/metrics?type={metric}&limit=50",
                        name="/metrics [by_type]")

    @task(1)
    def read_strain(self):
        self.client.get("/strain", name="/strain")
