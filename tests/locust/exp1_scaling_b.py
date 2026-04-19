"""
Experiment 1B — Horizontal Scaling: Fixed Load Comparison

Goal: Run a fixed 600-user load across 1, 2, 4 ECS tasks to measure
      whether horizontal scaling reduces P95 latency and failure rate.

Run (same command each time, change ecs_count in Terraform between runs):
    locust -f exp1_scaling_b.py --host http://<ALB_DNS> \
           --users 600 --spawn-rate 20 --run-time 5m \
           --html results/exp1b_t1.html

Scale tasks between runs:
    cd terraform && terraform apply -var="ecs_count=2" -auto-approve
    locust -f exp1_scaling_b.py ... --html results/exp1b_t2.html

    cd terraform && terraform apply -var="ecs_count=4" -auto-approve
    locust -f exp1_scaling_b.py ... --html results/exp1b_t4.html

Compare across runs:
    - Failure count (504s) — should drop toward zero as tasks increase
    - P95 /ingest/json latency — should drop proportionally if scaling is effective
    - Req/s — should increase with task count
    - If P95 stays high: split-brain problem (each task has independent rolling window)
"""

import random
from datetime import datetime, timezone

from locust import HttpUser, between, task


def hae_payload(metric_name: str, qty: float, unit: str) -> dict:
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
    cfg = {
        "heart_rate": ("heart_rate", (45, 185), "bpm"),
        "steps":      ("step_count", (0, 300),  "count"),
        "calories":   ("active_energy", (0, 25), "kcal"),
    }
    name, (lo, hi), unit = cfg.get(metric_type, (metric_type, (0, 100), ""))
    return hae_payload(name, round(random.uniform(lo, hi), 2), unit)


class IngestUser(HttpUser):
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
