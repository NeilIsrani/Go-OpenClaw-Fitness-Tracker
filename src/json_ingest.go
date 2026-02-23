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

// HandleIngestJSON handles POST /ingest/json from Health Auto Export app
func (app *App) HandleIngestJSON(c *gin.Context) {
	var export haeExport
	if err := c.ShouldBindJSON(&export); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	ingested := 0
	anomaliesDetected := 0

	// Process metrics
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
			app.Store.Add(record)
			app.Broker.Broadcast(SSEEvent{Event: "metric", Data: record})
			if anomaly := app.Detector.Detect(record); anomaly != nil {
				app.Store.AddAnomaly(*anomaly)
				anomaliesDetected++
				app.Broker.Broadcast(SSEEvent{Event: "anomaly", Data: anomaly})
			}
			ingested++
		}
	}

	// Process workouts
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
		app.Store.Add(record)
		app.Broker.Broadcast(SSEEvent{Event: "metric", Data: record})
		ingested++
	}

	log.Printf("[json] Ingested %d records, %d anomalies", ingested, anomaliesDetected)
	c.JSON(http.StatusOK, gin.H{
		"message":   "ok",
		"records":   ingested,
		"anomalies": anomaliesDetected,
	})
}
