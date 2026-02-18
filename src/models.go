package main

import "time"

// MetricType represents the type of health metric
type MetricType string

const (
	HeartRate MetricType = "heart_rate"
	Steps     MetricType = "steps"
	Calories  MetricType = "calories"
	Workout   MetricType = "workout"
)

// HealthRecord represents a single health data point
type HealthRecord struct {
	Type      MetricType `json:"type"`
	Value     float64    `json:"value"`
	Unit      string     `json:"unit"`
	Timestamp time.Time  `json:"timestamp"`
	Source    string     `json:"source,omitempty"`
	// Workout-specific fields
	WorkoutType string  `json:"workout_type,omitempty"`
	Duration    float64 `json:"duration,omitempty"` // seconds
}

// AnomalyEvent represents a detected anomaly
type AnomalyEvent struct {
	Type      MetricType `json:"type"`
	Value     float64    `json:"value"`
	Mean      float64    `json:"mean"`
	StdDev    float64    `json:"stddev"`
	ZScore    float64    `json:"zscore"`
	Severity  string     `json:"severity"` // "warning" or "critical"
	Timestamp time.Time  `json:"timestamp"`
}

// StatsResponse contains aggregate statistics for a metric type
type StatsResponse struct {
	Type   MetricType `json:"type"`
	Count  int        `json:"count"`
	Min    float64    `json:"min"`
	Max    float64    `json:"max"`
	Mean   float64    `json:"mean"`
	StdDev float64    `json:"stddev"`
	From   time.Time  `json:"from"`
	To     time.Time  `json:"to"`
}

// SSEEvent is a wrapper for events sent over SSE
type SSEEvent struct {
	Event string      `json:"event"` // "metric" or "anomaly"
	Data  interface{} `json:"data"`
}

// StrainScore represents a daily strain score derived from heart rate data
type StrainScore struct {
	Date           string  `json:"date"`            // YYYY-MM-DD
	Score          float64 `json:"score"`           // 0-21 scale
	MaxHR          float64 `json:"max_hr"`
	AvgHR          float64 `json:"avg_hr"`
	HRSamples      int     `json:"hr_samples"`
	TimeInZone1    float64 `json:"zone1_minutes"`   // 50-60% max HR (light)
	TimeInZone2    float64 `json:"zone2_minutes"`   // 60-70% max HR (moderate)
	TimeInZone3    float64 `json:"zone3_minutes"`   // 70-80% max HR (vigorous)
	TimeInZone4    float64 `json:"zone4_minutes"`   // 80-90% max HR (hard)
	TimeInZone5    float64 `json:"zone5_minutes"`   // 90-100% max HR (max)
	WorkoutCount   int     `json:"workout_count"`
	CaloriesBurned float64 `json:"calories_burned"`
}

// HealthCheckResponse is the health endpoint response
type HealthCheckResponse struct {
	Status      string `json:"status"`
	Records     int    `json:"records"`
	SSEClients  int    `json:"sse_clients"`
	Anomalies   int    `json:"anomalies"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}
