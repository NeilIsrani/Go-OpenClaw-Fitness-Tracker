package main

import (
	"encoding/xml"
	"io"
	"strconv"
	"time"
)

// Apple Health XML element types
type xmlRecord struct {
	Type          string `xml:"type,attr"`
	Value         string `xml:"value,attr"`
	Unit          string `xml:"unit,attr"`
	StartDate     string `xml:"startDate,attr"`
	SourceName    string `xml:"sourceName,attr"`
}

type xmlWorkout struct {
	WorkoutActivityType string  `xml:"workoutActivityType,attr"`
	Duration            string  `xml:"duration,attr"`
	DurationUnit        string  `xml:"durationUnit,attr"`
	TotalEnergyBurned   string  `xml:"totalEnergyBurned,attr"`
	TotalEnergyBurnedUnit string `xml:"totalEnergyBurnedUnit,attr"`
	StartDate           string  `xml:"startDate,attr"`
	SourceName          string  `xml:"sourceName,attr"`
}

// Apple Health date format
const appleHealthDateFormat = "2006-01-02 15:04:05 -0700"

// Mapping from Apple Health type identifiers to our MetricType
var metricTypeMap = map[string]MetricType{
	"HKQuantityTypeIdentifierHeartRate":                 HeartRate,
	"HKQuantityTypeIdentifierStepCount":                 Steps,
	"HKQuantityTypeIdentifierActiveEnergyBurned":        Calories,
	"HKQuantityTypeIdentifierBasalEnergyBurned":         Calories,
	"HKQuantityTypeIdentifierDietaryEnergyConsumed":     Calories,
}

// ParseAppleHealthXML streams Apple Health XML and sends parsed records to a channel.
// The channel is closed when parsing completes.
func ParseAppleHealthXML(reader io.Reader) <-chan HealthRecord {
	ch := make(chan HealthRecord, 1024)

	go func() {
		defer close(ch)
		decoder := xml.NewDecoder(reader)

		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}

			se, ok := token.(xml.StartElement)
			if !ok {
				continue
			}

			switch se.Name.Local {
			case "Record":
				var rec xmlRecord
				if err := decoder.DecodeElement(&rec, &se); err != nil {
					continue
				}
				metricType, known := metricTypeMap[rec.Type]
				if !known {
					continue
				}
				value, err := strconv.ParseFloat(rec.Value, 64)
				if err != nil {
					continue
				}
				ts, err := time.Parse(appleHealthDateFormat, rec.StartDate)
				if err != nil {
					continue
				}
				ch <- HealthRecord{
					Type:      metricType,
					Value:     value,
					Unit:      rec.Unit,
					Timestamp: ts,
					Source:    rec.SourceName,
				}

			case "Workout":
				var w xmlWorkout
				if err := decoder.DecodeElement(&w, &se); err != nil {
					continue
				}
				ts, err := time.Parse(appleHealthDateFormat, w.StartDate)
				if err != nil {
					continue
				}
				duration, _ := strconv.ParseFloat(w.Duration, 64)
				calories, _ := strconv.ParseFloat(w.TotalEnergyBurned, 64)

				ch <- HealthRecord{
					Type:        Workout,
					Value:       calories,
					Unit:        w.TotalEnergyBurnedUnit,
					Timestamp:   ts,
					Source:      w.SourceName,
					WorkoutType: w.WorkoutActivityType,
					Duration:    duration,
				}
			}
		}
	}()

	return ch
}
