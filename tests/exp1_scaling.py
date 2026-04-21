"""
Experiment 1 — Horizontal Scaling: Ingest Throughput vs ECS Task Count

Goal: Find the single-task CPU/memory ceiling by ramping users until latency breaks,
      then determine whether adding ECS tasks improves throughput.

Run (step-load to find the ceiling — replaces the fixed 90-user run):
    locust -f exp1_scaling.py --host http://<ALB_DNS> \
           --run-time 18m \
           --html results/exp1_stepload_t1.html

The StepLoad shape ramps 100→200→300→400→500→600 users in 3-minute steps.
The point where P95 breaks is the single-task ceiling.

Original fixed-load run (still valid for cross-task comparison once ceiling is known):
    locust -f exp1_scaling.py --host http://<ALB_DNS> \
           --users 90 --spawn-rate 10 --run-time 5m \
           --class-picker \
           --html results/exp1_fixed_t1.html

Repeat with ECS task count = 1, 2, 4, 8 via:
    terraform apply -var="ecs_count=N"

User mix:
    IngestUser  (weight=7)  — POST synthetic readings  → drives ~83 req/s at 90 users
    ReadUser    (weight=3)  — GET metrics/stats/anomalies → background read load
    StepLoad                — LoadTestShape: ramps 100→600 users in 3-min steps
"""

import json
import random
import time
from datetime import datetime, timezone

from locust import HttpUser, LoadTestShape, between, events, tag, task


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


class StepLoad(LoadTestShape):
    """
    Ramps users in steps of 200 every 3 minutes up to 2000.
    Use this shape to find the single-task CPU/memory ceiling.

    The test stops automatically when max_users is reached and the
    final step completes. Watch the Locust charts for the step where
    P95 ingest latency breaks — that is the ceiling.

    Run with:
        locust -f exp1_scaling.py --host http://<ALB_DNS> --run-time 18m
    Do NOT pass --users or --spawn-rate — StepLoad controls both.
    """
    step_users = 200       # add this many users per step
    step_duration = 180    # hold each step for 3 minutes (seconds)
    spawn_rate = 50        # users added per second during ramp
    max_users = 2000

    def tick(self):
        run_time = self.get_run_time()
        current_step = int(run_time // self.step_duration)
        users = min((current_step + 1) * self.step_users, self.max_users)

        # Stop after the final step completes
        if run_time >= self.step_duration * (self.max_users // self.step_users):
            return None

        return (users, self.spawn_rate)
