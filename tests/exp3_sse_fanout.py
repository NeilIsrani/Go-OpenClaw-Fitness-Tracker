"""
Experiment 3 — SSE Fan-Out: Real-Time Broadcast Ceiling

Goal: Find the fan-out ceiling of the SSE broker under concurrent persistent
      connections while anomaly events are being generated continuously.
      Verify the SSE bulkhead fires at 500 clients. Capture the reconnection
      storm behaviour by injecting ECS chaos automatically at the 4-minute mark.

Run:
    locust -f exp3_sse_fanout.py --host http://<ALB_DNS> \
           --users 565 --spawn-rate 5 --run-time 12m \
           --html results/exp3_fanout.html

Chaos injection fires automatically at 4 minutes via the test_start event hook.
Set ECS_CLUSTER and ECS_SERVICE env vars to enable it:
    ECS_CLUSTER=fitness-telemetry-cluster \
    ECS_SERVICE=fitness-telemetry-service \
    locust -f exp3_sse_fanout.py ...

User mix (565 total):
    SSEClientUser       (weight=9, ~495)  — holds /stream open 20-40s
    AnomalyDriverUser   (weight=1, ~55)   — POSTs extreme HR to trigger anomalies
    BulkheadProbeUser   (weight=1, ~15)   — connects after 500 limit, expects 503
"""

import os
import random
import threading
import time
from datetime import datetime, timezone

from locust import HttpUser, between, events, task


# Heart rate values that reliably trigger critical anomalies (z >= 3.0)
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


# ── Chaos injection ───────────────────────────────────────────────────────────

@events.test_start.add_listener
def on_test_start(environment, **kwargs):
    """
    Fires at test start. Schedules an ECS task-kill at the 4-minute mark.
    Requires ECS_CLUSTER and ECS_SERVICE environment variables.
    If boto3 is not installed or env vars are unset, logs a warning and skips.
    """
    cluster = os.environ.get("ECS_CLUSTER", "")
    service = os.environ.get("ECS_SERVICE", "")

    if not cluster or not service:
        print("[chaos] ECS_CLUSTER / ECS_SERVICE not set — chaos injection disabled")
        return

    try:
        import boto3
    except ImportError:
        print("[chaos] boto3 not installed — chaos injection disabled")
        print("[chaos] install with: pip install boto3")
        return

    def inject_chaos():
        delay = 240  # 4 minutes into the test
        print(f"[chaos] scheduled — will halve ECS task count in {delay}s")
        time.sleep(delay)

        try:
            ecs = boto3.client("ecs", region_name=os.environ.get("AWS_DEFAULT_REGION", "us-west-2"))
            svc = ecs.describe_services(cluster=cluster, services=[service])["services"][0]
            current_count = svc["desiredCount"]
            new_count = max(1, current_count // 2)

            ecs.update_service(
                cluster=cluster,
                service=service,
                desiredCount=new_count,
            )
            print(f"[chaos] killed {current_count - new_count} tasks: {current_count} → {new_count}")
            print("[chaos] watch Locust failure rate for the reconnection storm")
        except Exception as e:
            print(f"[chaos] injection failed: {e}")

    threading.Thread(target=inject_chaos, daemon=True).start()


# ── User classes ──────────────────────────────────────────────────────────────

class SSEClientUser(HttpUser):
    """
    Simulates a dashboard client holding a persistent SSE connection.
    The connection is held for 20-40 seconds per task to simulate a real user
    watching their dashboard.
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
            if resp.status_code == 503:
                # Bulkhead fired — not an error for SSEClientUser, just retry
                resp.failure(f"SSE capacity full (503) — retry")
                return
            if resp.status_code != 200:
                resp.failure(f"SSE connect failed: {resp.status_code}")
                return

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
    wait_time = between(0.1, 0.3)  # fast — ~4-8 anomaly triggers/second

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
        """Occasional normal reading so the window does not become all-anomalies."""
        self.client.post(
            "/ingest/json",
            json=normal_reading(),
            name="/ingest/json [normal]",
        )


class BulkheadProbeUser(HttpUser):
    """
    Verifies the SSE bulkhead fires correctly once 500 clients are connected.
    Expects HTTP 503 — a 503 response is treated as SUCCESS for this user class.
    A 200 response means the bulkhead is not enforcing the limit correctly.

    Run with 565+ total users so this class spawns after SSEClientUser has
    filled the 500-client cap.
    """
    weight = 1
    wait_time = between(5, 10)

    @task
    def probe_at_capacity(self):
        with self.client.get(
            "/stream",
            stream=True,
            catch_response=True,
            name="/stream [bulkhead probe]",
            timeout=5,
        ) as resp:
            if resp.status_code == 503:
                resp.success()  # 503 is the expected behaviour — bulkhead working
            elif resp.status_code == 200:
                # Connected past the limit — bulkhead not enforcing
                resp.failure(f"Bulkhead miss: got 200, expected 503")
                try:
                    resp.iter_content().__next__()  # drain so connection closes
                except Exception:
                    pass
            else:
                resp.failure(f"Unexpected status: {resp.status_code}")
