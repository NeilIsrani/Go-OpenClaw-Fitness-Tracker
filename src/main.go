package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed dashboard.html
var dashboardFS embed.FS

func main() {
	store := NewMetricStore()
	broker := NewSSEBroker()
	detector := NewAnomalyDetector()

	app := &App{
		Store:     store,
		Broker:    broker,
		Detector:  detector,
		StartTime: time.Now(),
	}

	router := gin.Default()

	// Serve dashboard
	router.GET("/", func(c *gin.Context) {
		data, err := dashboardFS.ReadFile("dashboard.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "dashboard not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	// Ingest endpoint: upload Apple Health XML
	router.POST("/ingest", func(c *gin.Context) {
		file, _, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file upload required, use multipart form field 'file'"})
			return
		}
		defer file.Close()

		// Create a pipe so we can stream the upload through the parser
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			io.Copy(pw, file)
		}()

		recordCh := ParseAppleHealthXML(pr)

		ingested := 0
		anomaliesDetected := 0

		for record := range recordCh {
			store.Add(record)
			ingested++

			// Broadcast to SSE clients
			broker.Broadcast(SSEEvent{
				Event: "metric",
				Data:  record,
			})

			// Check for anomalies
			if anomaly := detector.Detect(record); anomaly != nil {
				store.AddAnomaly(*anomaly)
				anomaliesDetected++
				broker.Broadcast(SSEEvent{
					Event: "anomaly",
					Data:  anomaly,
				})
			}
		}

		log.Printf("Ingested %d records, detected %d anomalies", ingested, anomaliesDetected)
		c.JSON(http.StatusOK, gin.H{
			"message":   fmt.Sprintf("ingested %d records", ingested),
			"records":   ingested,
			"anomalies": anomaliesDetected,
		})
	})

	// SSE streaming endpoint
	router.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")

		clientCh := broker.Subscribe()
		defer broker.Unsubscribe(clientCh)

		c.Stream(func(w io.Writer) bool {
			select {
			case msg, ok := <-clientCh:
				if !ok {
					return false
				}
				c.SSEvent("message", msg)
				return true
			case <-c.Request.Context().Done():
				return false
			}
		})
	})

	// JSON ingest for Health Auto Export app
	router.POST("/ingest/json", app.HandleIngestJSON)

	// REST API
	router.GET("/metrics", app.HandleGetMetrics)
	router.GET("/metrics/stats", app.HandleGetStats)
	router.GET("/metrics/all", app.HandleGetAllStats)
	router.GET("/anomalies", app.HandleGetAnomalies)
	router.GET("/strain", app.HandleGetStrain)
	router.GET("/health", app.HandleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Fitness Telemetry server starting on :%s", port)
	router.Run(":" + port)
}
