"""
Experiment 4 — Worker Pool Saturation: Queue Backpressure Ceiling

Goal: Find the point at which the 10-worker goroutine pool and 1024-slot
      ChannelQueue (or SQS queue) cannot keep up with ingest load.
      The signal is the `dropped` counter in the 202 response body turning
      non-zero — indicating the queue is full and records are being discarded.

Run (zero-wait burst to flood the queue):
    locust -f exp4_worker_saturation.py --host http://<ALB_DNS> \
           --users 200 --spawn-rate 20 --run-time 5m \
           --html results/exp4_saturation.html

Interpret results:
    - While dropped=0: workers are keeping up — the queue is draining faster
      than it fills. Increase users or batch size.
    - When dropped>0 appears: the queue is saturating. Note the user count
      and req/s at that point — that is the worker pool ceiling.
    - After dropped appears: reduce users and observe recovery. Fast recovery
      means the workers clear the backlog quickly.

Tuning:
    Adjust workerCount in src/queue.go and re-run to show the effect.
    Expected: doubling workerCount from 10 to 20 should roughly double the
    saturation ceiling (up to the point where Store.Add RWMutex becomes the limit).

User mix:
    BurstIngestUser     (weight=9)  — zero-wait, 50-reading batches, checks dropped
    DrainMonitorUser    (weight=1)  — polls /health to watch queue processing lag
"""

import random
import time
from datetime import datetime, timezone

from locust import HttpUser, between, constant, task


def large_batch(metric_name: str, count: int = 50) -> dict:
    """HAE-format payload with `count` readings — 10x the normal batch size."""
    cfg = {
        "heart_rate":    ("heart_rate",    (55, 180), "bpm"),
        "step_count":    ("step_count",    (0, 300),  "count"),
        "active_energy": ("active_energy", (0, 25),   "kcal"),
    }
    name, (lo, hi), unit = cfg[metric_name]
    date_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S +0000")
    readings = [
        {"qty": round(random.uniform(lo, hi), 2), "date": date_str}
        for _ in range(count)
    ]
    return {
        "data": {
            "metrics": [{"name": name, "units": unit, "data": readings}],
            "workouts": [],
        }
    }


METRICS = ["heart_rate", "step_count", "active_energy"]


class BurstIngestUser(HttpUser):
    """
    Sends maximum-size batches (50 readings) with no wait between requests.
    Designed to outpace the worker pool and fill the queue.

    Checks the `dropped` field in each 202 response.
    A non-zero dropped count is marked as a Locust failure so it appears
    in the failure rate chart — the saturation signal.
    """
    weight = 9
    wait_time = constant(0)  # no pause — maximum sustained pressure

    @task
    def burst_ingest(self):
        metric = random.choice(METRICS)
        payload = large_batch(metric, count=50)

        with self.client.post(
            "/ingest/json",
            json=payload,
            catch_response=True,
            name="/ingest/json [burst 50]",
        ) as resp:
            if resp.status_code != 202:
                resp.failure(f"Expected 202, got {resp.status_code}")
                return

            try:
                data = resp.json()
                dropped = data.get("dropped", 0)
                if dropped > 0:
                    # Queue is saturating — mark as failure so it shows in charts
                    resp.failure(f"queue_full: dropped={dropped}")
                else:
                    resp.success()
            except Exception as e:
                resp.failure(f"response parse error: {e}")


class DrainMonitorUser(HttpUser):
    """
    Polls /health every 2 seconds to track total records processed.
    Comparing records count over time reveals the worker throughput rate
    and whether the queue is draining between bursts.

    Watch the `records` field in the Locust response body tab — it should
    grow steadily. If it stalls while burst users are active, workers are stuck.
    """
    weight = 1
    wait_time = between(2, 3)

    def on_start(self):
        self._last_records = 0
        self._last_time = time.time()

    @task
    def check_drain_rate(self):
        with self.client.get(
            "/health",
            catch_response=True,
            name="/health [drain monitor]",
        ) as resp:
            if resp.status_code != 200:
                resp.failure(f"health check failed: {resp.status_code}")
                return

            try:
                data = resp.json()
                now = time.time()
                records = data.get("records", 0)
                elapsed = now - self._last_time
                rate = (records - self._last_records) / elapsed if elapsed > 0 else 0

                self._last_records = records
                self._last_time = now

                # Log drain rate — visible in Locust output / --loglevel DEBUG
                if rate < 10 and records > 0:
                    resp.failure(f"drain stall: {rate:.1f} records/s (total={records})")
                else:
                    resp.success()
            except Exception as e:
                resp.failure(f"parse error: {e}")
