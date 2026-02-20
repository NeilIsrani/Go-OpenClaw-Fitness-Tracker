"""
Generate synthetic Apple Health XML for testing the /ingest endpoint.
Usage: python gen_xml.py > synthetic_health.xml
"""

import random
import math
from datetime import datetime, timedelta

DAYS = 7
START = datetime.now() - timedelta(days=DAYS)
SOURCE = "Apple Watch"

DATE_FMT = "%Y-%m-%d %H:%M:%S +0000"


def fmt(dt):
    return dt.strftime(DATE_FMT)


records = []

# Heart rate: every 5 minutes, 55-85 bpm with occasional anomaly spikes
t = START
while t < datetime.now():
    # inject a spike anomaly every ~200 readings
    if random.random() < 0.005:
        bpm = random.uniform(160, 185)
    else:
        bpm = random.gauss(68, 8)
        bpm = max(45, min(120, bpm))
    records.append(
        f'  <Record type="HKQuantityTypeIdentifierHeartRate" '
        f'value="{bpm:.1f}" unit="count/min" '
        f'startDate="{fmt(t)}" sourceName="{SOURCE}"/>'
    )
    t += timedelta(minutes=5)

# Steps: hourly buckets, 0 overnight, ~500-1500 during day
t = START
while t < datetime.now():
    hour = t.hour
    if 6 <= hour <= 22:
        steps = random.gauss(800, 300)
        steps = max(0, steps)
    else:
        steps = random.uniform(0, 50)
    records.append(
        f'  <Record type="HKQuantityTypeIdentifierStepCount" '
        f'value="{steps:.0f}" unit="count" '
        f'startDate="{fmt(t)}" sourceName="{SOURCE}"/>'
    )
    t += timedelta(hours=1)

# Active calories: every 30 minutes
t = START
while t < datetime.now():
    hour = t.hour
    if 6 <= hour <= 22:
        cal = random.gauss(40, 15)
        cal = max(0, cal)
    else:
        cal = random.uniform(0, 10)
    records.append(
        f'  <Record type="HKQuantityTypeIdentifierActiveEnergyBurned" '
        f'value="{cal:.1f}" unit="Cal" '
        f'startDate="{fmt(t)}" sourceName="{SOURCE}"/>'
    )
    t += timedelta(minutes=30)

# Basal calories: every hour, ~60-80 Cal
t = START
while t < datetime.now():
    cal = random.gauss(70, 5)
    records.append(
        f'  <Record type="HKQuantityTypeIdentifierBasalEnergyBurned" '
        f'value="{cal:.1f}" unit="Cal" '
        f'startDate="{fmt(t)}" sourceName="{SOURCE}"/>'
    )
    t += timedelta(hours=1)

# Workouts: 1 per day, alternating run/cycle
workout_types = [
    "HKWorkoutActivityTypeRunning",
    "HKWorkoutActivityTypeCycling",
    "HKWorkoutActivityTypeWalking",
]
for i in range(DAYS):
    wt = START + timedelta(days=i, hours=7, minutes=30)
    duration = random.uniform(20, 60)
    calories = duration * random.uniform(8, 12)
    wtype = workout_types[i % len(workout_types)]
    records.append(
        f'  <Workout workoutActivityType="{wtype}" '
        f'duration="{duration:.1f}" durationUnit="min" '
        f'totalEnergyBurned="{calories:.1f}" totalEnergyBurnedUnit="Cal" '
        f'startDate="{fmt(wt)}" sourceName="{SOURCE}"/>'
    )

random.shuffle(records)

print('<?xml version="1.0" encoding="UTF-8"?>')
print('<HealthData locale="en_US">')
for r in records:
    print(r)
print('</HealthData>')
