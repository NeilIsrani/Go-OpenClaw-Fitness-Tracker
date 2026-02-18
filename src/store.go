package main

import (
	"math"
	"sort"
	"sync"
	"time"
)

// MetricStore is an in-memory time-series store using sorted slices and binary search
type MetricStore struct {
	mu        sync.RWMutex
	data      map[MetricType][]HealthRecord
	anomalies []AnomalyEvent
}

// NewMetricStore creates a new empty store
func NewMetricStore() *MetricStore {
	return &MetricStore{
		data: make(map[MetricType][]HealthRecord),
	}
}

// Add inserts a record into the store, maintaining sorted order by timestamp
func (s *MetricStore) Add(record HealthRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := s.data[record.Type]
	// Find insertion point using binary search to maintain sorted order
	i := sort.Search(len(records), func(j int) bool {
		return records[j].Timestamp.After(record.Timestamp)
	})
	// Insert at position i
	records = append(records, HealthRecord{})
	copy(records[i+1:], records[i:])
	records[i] = record
	s.data[record.Type] = records
}

// AddAnomaly stores a detected anomaly
func (s *MetricStore) AddAnomaly(a AnomalyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.anomalies = append(s.anomalies, a)
}

// Query returns records of a given type within a time range, with optional limit
func (s *MetricStore) Query(metricType MetricType, from, to time.Time, limit int) []HealthRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := s.data[metricType]
	if len(records) == 0 {
		return nil
	}

	// Binary search for the start of the range
	start := sort.Search(len(records), func(i int) bool {
		return !records[i].Timestamp.Before(from)
	})

	var result []HealthRecord
	for i := start; i < len(records); i++ {
		if records[i].Timestamp.After(to) {
			break
		}
		result = append(result, records[i])
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

// GetAll returns all records of a given type, with optional limit
func (s *MetricStore) GetAll(metricType MetricType, limit int) []HealthRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := s.data[metricType]
	if limit > 0 && limit < len(records) {
		return records[len(records)-limit:]
	}
	result := make([]HealthRecord, len(records))
	copy(result, records)
	return result
}

// Stats computes aggregate statistics for a metric type within a time range
func (s *MetricStore) Stats(metricType MetricType, from, to time.Time) *StatsResponse {
	records := s.Query(metricType, from, to, 0)
	if len(records) == 0 {
		return nil
	}

	minVal := math.MaxFloat64
	maxVal := -math.MaxFloat64
	sum := 0.0
	sumSq := 0.0

	for _, r := range records {
		if r.Value < minVal {
			minVal = r.Value
		}
		if r.Value > maxVal {
			maxVal = r.Value
		}
		sum += r.Value
		sumSq += r.Value * r.Value
	}

	n := float64(len(records))
	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	if variance < 0 {
		variance = 0
	}
	stddev := math.Sqrt(variance)

	return &StatsResponse{
		Type:   metricType,
		Count:  len(records),
		Min:    minVal,
		Max:    maxVal,
		Mean:   math.Round(mean*100) / 100,
		StdDev: math.Round(stddev*100) / 100,
		From:   records[0].Timestamp,
		To:     records[len(records)-1].Timestamp,
	}
}

// GetAnomalies returns all detected anomalies, optionally filtered by type
func (s *MetricStore) GetAnomalies(metricType MetricType) []AnomalyEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if metricType == "" {
		result := make([]AnomalyEvent, len(s.anomalies))
		copy(result, s.anomalies)
		return result
	}

	var result []AnomalyEvent
	for _, a := range s.anomalies {
		if a.Type == metricType {
			result = append(result, a)
		}
	}
	return result
}

// TotalRecords returns the total count of all records across all types
func (s *MetricStore) TotalRecords() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, records := range s.data {
		total += len(records)
	}
	return total
}
