package main

import (
	"math"
	"time"
)

// Default max HR estimate (220 - age). Users can override via query param.
const defaultMaxHR = 190.0

// HR zone thresholds as percentages of max HR
var zoneThresholds = [5][2]float64{
	{0.50, 0.60}, // Zone 1: light
	{0.60, 0.70}, // Zone 2: moderate
	{0.70, 0.80}, // Zone 3: vigorous
	{0.80, 0.90}, // Zone 4: hard
	{0.90, 1.00}, // Zone 5: max
}

// Zone strain multipliers — higher zones contribute exponentially more strain
var zoneMultipliers = [5]float64{0.2, 0.5, 1.0, 2.5, 5.0}

// CalculateStrainScore computes a daily strain score from heart rate data.
// Strain is modeled as a logarithmic accumulation of time spent in HR zones,
// scaled to a 0-21 range (similar to Whoop's strain model).
func CalculateStrainScore(store *MetricStore, date string, maxHR float64) *StrainScore {
	if maxHR <= 0 {
		maxHR = defaultMaxHR
	}

	// Parse date to get start/end of day
	dayStart, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil
	}
	dayEnd := dayStart.Add(24 * time.Hour)

	// Query heart rate data for the day
	hrRecords := store.Query(HeartRate, dayStart, dayEnd, 0)
	if len(hrRecords) == 0 {
		return &StrainScore{Date: date, Score: 0}
	}

	// Query workouts and calories for the day
	workouts := store.Query(Workout, dayStart, dayEnd, 0)
	calRecords := store.Query(Calories, dayStart, dayEnd, 0)

	totalCalories := 0.0
	for _, c := range calRecords {
		totalCalories += c.Value
	}

	// Calculate time in each HR zone
	var zoneMins [5]float64
	sumHR := 0.0
	peakHR := 0.0

	// Estimate time between samples (assume ~1 min per sample for Apple Watch)
	sampleIntervalMin := 1.0
	if len(hrRecords) > 1 {
		totalDuration := hrRecords[len(hrRecords)-1].Timestamp.Sub(hrRecords[0].Timestamp).Minutes()
		if totalDuration > 0 {
			sampleIntervalMin = totalDuration / float64(len(hrRecords)-1)
			if sampleIntervalMin > 10 {
				sampleIntervalMin = 10 // cap at 10 min if samples are sparse
			}
		}
	}

	for _, r := range hrRecords {
		hr := r.Value
		sumHR += hr
		if hr > peakHR {
			peakHR = hr
		}

		// Classify into zone
		hrPct := hr / maxHR
		for z := 4; z >= 0; z-- { // check highest zone first
			if hrPct >= zoneThresholds[z][0] {
				zoneMins[z] += sampleIntervalMin
				break
			}
		}
	}

	avgHR := sumHR / float64(len(hrRecords))

	// Calculate raw strain from zone contributions
	rawStrain := 0.0
	for z := 0; z < 5; z++ {
		rawStrain += zoneMins[z] * zoneMultipliers[z]
	}

	// Apply logarithmic scaling to get 0-21 range
	// Using ln(1 + rawStrain) scaled so that a very hard day (~300 raw) maps to ~21
	score := 21.0 * math.Log(1+rawStrain) / math.Log(1+300)
	if score > 21.0 {
		score = 21.0
	}
	score = math.Round(score*10) / 10

	return &StrainScore{
		Date:           date,
		Score:          score,
		MaxHR:          peakHR,
		AvgHR:          math.Round(avgHR*10) / 10,
		HRSamples:      len(hrRecords),
		TimeInZone1:    math.Round(zoneMins[0]*10) / 10,
		TimeInZone2:    math.Round(zoneMins[1]*10) / 10,
		TimeInZone3:    math.Round(zoneMins[2]*10) / 10,
		TimeInZone4:    math.Round(zoneMins[3]*10) / 10,
		TimeInZone5:    math.Round(zoneMins[4]*10) / 10,
		WorkoutCount:   len(workouts),
		CaloriesBurned: math.Round(totalCalories*10) / 10,
	}
}
