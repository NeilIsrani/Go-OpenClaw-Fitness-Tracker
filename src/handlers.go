package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// App holds shared dependencies for handlers
type App struct {
	Store     *MetricStore
	Broker    *SSEBroker
	Detector  *AnomalyDetector
	Queue     IngestQueue
	StartTime time.Time
}

// HandleGetMetrics handles GET /metrics?type=heart_rate&from=...&to=...&limit=100
func (app *App) HandleGetMetrics(c *gin.Context) {
	metricType := MetricType(c.Query("type"))
	if metricType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type parameter is required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "0")
	limit, _ := strconv.Atoi(limitStr)

	fromStr := c.Query("from")
	toStr := c.Query("to")

	if fromStr != "" && toStr != "" {
		from, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date, use RFC3339 format"})
			return
		}
		to, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date, use RFC3339 format"})
			return
		}
		records := app.Store.Query(metricType, from, to, limit)
		if records == nil {
			records = []HealthRecord{}
		}
		c.JSON(http.StatusOK, gin.H{"records": records, "count": len(records)})
		return
	}

	records := app.Store.GetAll(metricType, limit)
	if records == nil {
		records = []HealthRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"records": records, "count": len(records)})
}

// HandleGetStats handles GET /metrics/stats?type=heart_rate&from=...&to=...
func (app *App) HandleGetStats(c *gin.Context) {
	metricType := MetricType(c.Query("type"))
	if metricType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type parameter is required"})
		return
	}

	from := time.Time{}
	to := time.Now().Add(24 * time.Hour)

	fromStr := c.Query("from")
	toStr := c.Query("to")

	if fromStr != "" {
		parsed, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date"})
			return
		}
		from = parsed
	}
	if toStr != "" {
		parsed, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date"})
			return
		}
		to = parsed
	}

	stats := app.Store.Stats(metricType, from, to)
	if stats == nil {
		c.JSON(http.StatusOK, gin.H{"message": "no data for this metric type"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// HandleGetAnomalies handles GET /anomalies?type=heart_rate
func (app *App) HandleGetAnomalies(c *gin.Context) {
	metricType := MetricType(c.Query("type"))
	anomalies := app.Store.GetAnomalies(metricType)
	if anomalies == nil {
		anomalies = []AnomalyEvent{}
	}
	c.JSON(http.StatusOK, gin.H{"anomalies": anomalies, "count": len(anomalies)})
}

// HandleGetStrain handles GET /strain?date=2024-02-15&max_hr=190
func (app *App) HandleGetStrain(c *gin.Context) {
	date := c.DefaultQuery("date", time.Now().Format("2006-01-02"))
	maxHRStr := c.DefaultQuery("max_hr", "0")
	maxHR, _ := strconv.ParseFloat(maxHRStr, 64)

	strain := CalculateStrainScore(app.Store, date, maxHR)
	if strain == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYY-MM-DD"})
		return
	}
	c.JSON(http.StatusOK, strain)
}

// HandleGetAllStats handles GET /metrics/all - fans out stats queries concurrently across all metric types
func (app *App) HandleGetAllStats(c *gin.Context) {
	metricTypes := []MetricType{HeartRate, Steps, Calories}
	from := time.Time{}
	to := time.Now().Add(24 * time.Hour)

	type result struct {
		metricType MetricType
		stats      *StatsResponse
	}

	resultCh := make(chan result, len(metricTypes))

	var wg sync.WaitGroup
	for _, mt := range metricTypes {
		wg.Add(1)
		go func(mt MetricType) {
			defer wg.Done()
			stats := app.Store.Stats(mt, from, to)
			resultCh <- result{metricType: mt, stats: stats}
		}(mt)
	}

	// Close result channel once all goroutines finish
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Fan-in: collect results as they arrive
	all := make(map[MetricType]*StatsResponse)
	for r := range resultCh {
		all[r.metricType] = r.stats
	}

	c.JSON(http.StatusOK, all)
}

// HandleHealth handles GET /health
func (app *App) HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, HealthCheckResponse{
		Status:        "ok",
		Records:       app.Store.TotalRecords(),
		SSEClients:    app.Broker.ClientCount(),
		Anomalies:     len(app.Store.GetAnomalies("")),
		UptimeSeconds: time.Since(app.StartTime).Seconds(),
	})
}
