"""
Experiment 3 — SSE Fan-Out: Real-Time Broadcast Ceiling

Goal: Find the fan-out ceiling of the SSE broker under concurrent persistent
      connections while anomaly events are being generated continuously.
      Identify where the bottleneck sits: Redis pub/sub, ECS connection handling,
      or the ALB.

Run (ramp SSE clients while driving anomaly load):
    locust -f exp3_sse_fanout.py --host http://<ALB_DNS> \
           --users 550 --spawn-rate 5 --run-time 10m \
           --html results/exp3_fanout.html

Chaos injection: mid-test, kill 50% of ECS tasks via AWS Console / CLI:
    aws ecs update-service --cluster fitness-telemetry-cluster \
        --service fitness-telemetry-service --desired-count <half>
    Observe reconnection storm in Locust failure rate chart.

User mix:
    SSEClientUser       (weight=9)  — holds a persistent /stream connection open
    AnomalyDriverUser   (weight=1)  — POSTs extreme heart rate values to trigger anomalies
"""

import random
import time
from datetime import datetime, timezone

from locust import HttpUser, between, task


# Heart rate values that reliably trigger critical anomalies (z ≥ 3.0)
# after the rolling window is seeded with normal values.
ANOMALY_TRIGGERS = [220, 225, 230, 10, 8, 5]  # extreme high and low


def _hae(qty: float) -> dict:
    date_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S +0000")
    return {
        "data": {
            "metrics": [
                {"name": "heart_rate", "units": "bpm",
                 "data": [{"qty": qty, "date": date_str}]}
            ],
            "workouts": [],
        }
    }


def normal_reading(metric_type: str = "heart_rate") -> dict:
    return _hae(round(random.uniform(58, 82), 1))


def anomaly_reading() -> dict:
    return _hae(float(random.choice(ANOMALY_TRIGGERS)))


class SSEClientUser(HttpUser):
    """
    Simulates a dashboard client holding a persistent SSE connection.
    The connection is held for 20–40 seconds per task to simulate a real user
    watching their dashboard — much longer than the 3s timeout in the old locustfile.
    """
    weight = 9
    wait_time = between(1, 3)

    def on_start(self):
        # Seed the rolling window with normal values so anomaly detection is active
        for _ in range(10):
            self.client.post(
                "/ingest/json",
                json=normal_reading(),
                name="/ingest/json [seed]",
            )

    @task
    def hold_sse_connection(self):
        hold_seconds = random.uniform(20, 40)
        deadline = time.time() + hold_seconds
        event_count = 0

        with self.client.get(
            "/stream",
            stream=True,
            catch_response=True,
            name="/stream [SSE hold]",
            timeout=hold_seconds + 5,
        ) as resp:
            if resp.status_code != 200:
                resp.failure(f"SSE connect failed: {resp.status_code}")
                return

            # Consume SSE events for the hold duration
            try:
                for chunk in resp.iter_content(chunk_size=None):
                    if chunk:
                        event_count += 1
                    if time.time() >= deadline:
                        break
                resp.success()
            except Exception as e:
                resp.failure(f"SSE stream error after {event_count} events: {e}")


class AnomalyDriverUser(HttpUser):
    """
    Continuously injects extreme heart rate readings to drive anomaly events
    into the SSE stream. One driver per ~10 SSE clients keeps the stream active.
    """
    weight = 1
    wait_time = between(0.1, 0.3)  # fast — ~4–8 anomaly triggers/second

    def on_start(self):
        # Prime the rolling window with 50 normal readings first
        for _ in range(50):
            self.client.post(
                "/ingest/json",
                json=normal_reading(),
                name="/ingest/json [prime]",
            )

    @task(3)
    def trigger_anomaly(self):
        self.client.post(
            "/ingest/json",
            json=anomaly_reading(),
            name="/ingest/json [anomaly trigger]",
        )

    @task(1)
    def send_normal(self):
        """Occasional normal reading so the window doesn't become all-anomalies."""
        self.client.post(
            "/ingest/json",
            json=normal_reading(),
            name="/ingest/json [normal]",
        )
