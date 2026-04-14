package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Health Auto Export JSON format
type haeExport struct {
	Data haeData `json:"data"`
}

type haeData struct {
	Metrics  []haeMetric  `json:"metrics"`
	Workouts []haeWorkout `json:"workouts"`
}

type haeMetric struct {
	Name  string      `json:"name"`
	Units string      `json:"units"`
	Data  []haeReading `json:"data"`
}

type haeReading struct {
	Qty  float64 `json:"qty"`
	Date string  `json:"date"`
}

type haeWorkout struct {
	Name          string     `json:"name"`
	Start         string     `json:"start"`
	End           string     `json:"end"`
	Duration      string     `json:"duration"`
	ActiveEnergy  *haeQtyUnit `json:"activeEnergy"`
	TotalDistance *haeQtyUnit `json:"totalDistance"`
}

type haeQtyUnit struct {
	Qty   float64 `json:"qty"`
	Units string  `json:"units"`
}

// Health Auto Export metric name → our MetricType
var haeMetricMap = map[string]MetricType{
	"heart_rate":           HeartRate,
	"heartrate":            HeartRate,
	"step_count":           Steps,
	"steps":                Steps,
	"active_energy":        Calories,
	"activeenergy":         Calories,
	"basal_energy_burned":  Calories,
	"resting_energy":       Calories,
}

// Date formats Health Auto Export uses
var haeDateFormats = []string{
	"2006-01-02 15:04:05 -0700",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02 15:04:05",
	"01-02-2006 15:04:05",
}

func parseHAEDate(s string) (time.Time, bool) {
	for _, layout := range haeDateFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// HandleIngestJSON handles POST /ingest/json from Health Auto Export app.
// Parses the payload, enqueues each record, and returns 202 immediately.
// Store, anomaly detection, and SSE broadcast happen in background workers.
func (app *App) HandleIngestJSON(c *gin.Context) {
	var export haeExport
	if err := c.ShouldBindJSON(&export); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	queued := 0
	dropped := 0

	// Enqueue metrics
	for _, metric := range export.Data.Metrics {
		metricType, known := haeMetricMap[metric.Name]
		if !known {
			continue
		}
		for _, reading := range metric.Data {
			ts, ok := parseHAEDate(reading.Date)
			if !ok {
				continue
			}
			record := HealthRecord{
				Type:      metricType,
				Value:     reading.Qty,
				Unit:      metric.Units,
				Timestamp: ts,
				Source:    "Health Auto Export",
			}
			if err := app.Queue.Enqueue(record); err != nil {
				log.Printf("[json] queue full, dropping record: %v", err)
				dropped++
			} else {
				queued++
			}
		}
	}

	// Enqueue workouts
	for _, w := range export.Data.Workouts {
		ts, ok := parseHAEDate(w.Start)
		if !ok {
			continue
		}
		calories := 0.0
		if w.ActiveEnergy != nil {
			calories = w.ActiveEnergy.Qty
		}
		record := HealthRecord{
			Type:        Workout,
			Value:       calories,
			Unit:        "Cal",
			Timestamp:   ts,
			Source:      "Health Auto Export",
			WorkoutType: w.Name,
		}
		if err := app.Queue.Enqueue(record); err != nil {
			log.Printf("[json] queue full, dropping workout: %v", err)
			dropped++
		} else {
			queued++
		}
	}

	log.Printf("[json] queued=%d dropped=%d", queued, dropped)
	c.JSON(http.StatusAccepted, gin.H{
		"status":  "queued",
		"queued":  queued,
		"dropped": dropped,
	})
}
