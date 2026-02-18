import random
import time
from locust import HttpUser, FastHttpUser, task, between


class FitnessTelemetryUser(HttpUser):
    """Standard HttpUser for Fitness Telemetry API load testing."""
    wait_time = between(1, 3)

    @task(5)
    def get_heart_rate_metrics(self):
        """GET heart rate metrics (read-heavy workload)."""
        self.client.get("/metrics?type=heart_rate&limit=50",
                        name="/metrics?type=heart_rate")

    @task(3)
    def get_steps_metrics(self):
        """GET step count metrics."""
        self.client.get("/metrics?type=steps&limit=50",
                        name="/metrics?type=steps")

    @task(3)
    def get_calories_metrics(self):
        """GET calorie metrics."""
        self.client.get("/metrics?type=calories&limit=50",
                        name="/metrics?type=calories")

    @task(4)
    def get_heart_rate_stats(self):
        """GET heart rate statistics."""
        self.client.get("/metrics/stats?type=heart_rate",
                        name="/metrics/stats?type=heart_rate")

    @task(2)
    def get_steps_stats(self):
        """GET step count statistics."""
        self.client.get("/metrics/stats?type=steps",
                        name="/metrics/stats?type=steps")

    @task(3)
    def get_anomalies(self):
        """GET all anomalies."""
        self.client.get("/anomalies", name="/anomalies")

    @task(2)
    def get_anomalies_by_type(self):
        """GET anomalies filtered by type."""
        metric = random.choice(["heart_rate", "steps", "calories"])
        self.client.get(f"/anomalies?type={metric}",
                        name="/anomalies?type=[type]")

    @task(1)
    def get_workout_metrics(self):
        """GET workout records."""
        self.client.get("/metrics?type=workout&limit=20",
                        name="/metrics?type=workout")

    @task(2)
    def health_check(self):
        """GET health check endpoint."""
        self.client.get("/health", name="/health")


class FitnessFastUser(FastHttpUser):
    """FastHttpUser for higher throughput testing."""
    wait_time = between(1, 3)

    @task(5)
    def get_heart_rate_metrics(self):
        self.client.get("/metrics?type=heart_rate&limit=50",
                        name="/metrics?type=heart_rate (fast)")

    @task(4)
    def get_heart_rate_stats(self):
        self.client.get("/metrics/stats?type=heart_rate",
                        name="/metrics/stats?type=heart_rate (fast)")

    @task(3)
    def get_anomalies(self):
        self.client.get("/anomalies",
                        name="/anomalies (fast)")

    @task(2)
    def health_check(self):
        self.client.get("/health",
                        name="/health (fast)")


class SSEStreamUser(HttpUser):
    """Tests SSE streaming endpoint by connecting briefly."""
    wait_time = between(5, 10)

    @task
    def stream_events(self):
        """Connect to SSE stream for a brief period."""
        with self.client.get("/stream", stream=True, catch_response=True,
                             name="/stream (SSE)", timeout=3) as resp:
            if resp.status_code == 200:
                resp.success()
            else:
                resp.failure(f"SSE stream returned {resp.status_code}")
