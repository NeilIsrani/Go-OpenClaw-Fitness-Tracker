package main

import (
	"math"
	"sync"
	"time"
)

const (
	windowSize       = 200
	warningThreshold = 2.0
	criticalThreshold = 3.0
)

// AnomalyDetector uses a sliding window with running statistics for z-score computation
type AnomalyDetector struct {
	mu      sync.Mutex
	windows map[MetricType]*slidingWindow
}

type slidingWindow struct {
	values []float64
	sum    float64
	sumSq  float64
	pos    int
	full   bool
}

// NewAnomalyDetector creates a new detector
func NewAnomalyDetector() *AnomalyDetector {
	return &AnomalyDetector{
		windows: make(map[MetricType]*slidingWindow),
	}
}

// Detect checks if a record is anomalous. Returns an AnomalyEvent if so, nil otherwise.
func (d *AnomalyDetector) Detect(record HealthRecord) *AnomalyEvent {
	// Don't detect anomalies on workout records
	if record.Type == Workout {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	w, exists := d.windows[record.Type]
	if !exists {
		w = &slidingWindow{
			values: make([]float64, windowSize),
		}
		d.windows[record.Type] = w
	}

	// Need at least 10 data points before detecting anomalies
	count := w.count()
	if count >= 10 {
		mean := w.sum / float64(count)
		variance := (w.sumSq / float64(count)) - (mean * mean)
		if variance < 0 {
			variance = 0
		}
		stddev := math.Sqrt(variance)

		if stddev > 0 {
			zscore := math.Abs(record.Value-mean) / stddev
			if zscore >= warningThreshold {
				severity := "warning"
				if zscore >= criticalThreshold {
					severity = "critical"
				}
				event := &AnomalyEvent{
					Type:      record.Type,
					Value:     record.Value,
					Mean:      math.Round(mean*100) / 100,
					StdDev:    math.Round(stddev*100) / 100,
					ZScore:    math.Round(zscore*100) / 100,
					Severity:  severity,
					Timestamp: record.Timestamp,
				}
				// Still add the value to the window before returning
				w.add(record.Value)
				return event
			}
		}
	}

	w.add(record.Value)
	return nil
}

func (w *slidingWindow) add(value float64) {
	if w.full {
		// Remove the oldest value
		old := w.values[w.pos]
		w.sum -= old
		w.sumSq -= old * old
	}
	w.values[w.pos] = value
	w.sum += value
	w.sumSq += value * value
	w.pos = (w.pos + 1) % windowSize
	if w.pos == 0 {
		w.full = true
	}
}

func (w *slidingWindow) count() int {
	if w.full {
		return windowSize
	}
	return w.pos
}

// DetectRealTime checks a record and returns anomaly event with current time if detected
func (d *AnomalyDetector) DetectRealTime(record HealthRecord) *AnomalyEvent {
	event := d.Detect(record)
	if event != nil {
		event.Timestamp = time.Now()
	}
	return event
}
