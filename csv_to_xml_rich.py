"""
Generate Apple Health XML from 7 selected rich days in healthreport.csv.
Synthesizes per-reading heart rate spread from daily min/avg/max.
Usage: python3 csv_to_xml_rich.py > rich_health.xml
"""

import csv, random, sys
from datetime import datetime, timedelta

random.seed(42)

CSV_FILE  = "healthreport.csv"
SOURCE    = "Apple Watch"
DATE_FMT  = "%Y-%m-%d %H:%M:%S +0000"

SELECTED = {
    "2026-01-01", "2026-01-29", "2026-01-31",
    "2026-02-01", "2026-02-02", "2026-02-16", "2026-02-17",
}

# Workout days: (HK workout type, approx start hour, duration minutes, calories)
WORKOUTS = {
    "2026-01-31": ("HKWorkoutActivityTypeCycling",  8, 35, 180),
    "2026-02-01": ("HKWorkoutActivityTypeRunning",  7, 28, 290),
    "2026-02-16": ("HKWorkoutActivityTypeCycling",  9, 45, 390),
}

records = []

def ts(date_str, hour, minute=0):
    dt = datetime.strptime(date_str, "%Y-%m-%d")
    dt = dt.replace(hour=hour, minute=minute)
    return dt.strftime(DATE_FMT)

def rec(hk_type, value, unit, date_str, hour, minute=0):
    return (
        f'  <Record type="{hk_type}" value="{value}" unit="{unit}" '
        f'startDate="{ts(date_str, hour, minute)}" sourceName="{SOURCE}"/>'
    )

def workout(date_str, wtype, start_hour, duration, calories):
    dt = datetime.strptime(date_str, "%Y-%m-%d").replace(hour=start_hour)
    return (
        f'  <Workout workoutActivityType="{wtype}" '
        f'duration="{duration}" durationUnit="min" '
        f'totalEnergyBurned="{calories:.1f}" totalEnergyBurnedUnit="Cal" '
        f'startDate="{dt.strftime(DATE_FMT)}" sourceName="{SOURCE}"/>'
    )

def spread_hr(date_str, hr_min, hr_avg, hr_max, workout_hour=None):
    """Generate ~25 HR readings across the day matching the daily stats."""
    out = []
    # waking hours 7am-11pm, reading every ~35 min
    times = list(range(7*60, 23*60, 35))
    for t in times:
        h, m = divmod(t, 60)
        # near workout: spike toward max
        if workout_hour and abs(h - workout_hour) <= 1:
            val = random.uniform(hr_avg * 1.15, hr_max * 0.97)
        else:
            # gaussian around avg, clamped to [min, max]
            val = random.gauss(hr_avg, (hr_max - hr_min) / 5)
            val = max(hr_min * 0.95, min(hr_max * 0.95, val))
        out.append(rec("HKQuantityTypeIdentifierHeartRate",
                       f"{val:.1f}", "count/min", date_str, h, m))
    return out

with open(CSV_FILE, newline="", encoding="utf-8") as f:
    reader = csv.DictReader(f)
    for row in reader:
        d = row["Date/Time"].strip()[:10]
        if d not in SELECTED:
            continue

        def g(col): return row.get(col, "").strip()

        hr_avg = float(g("Heart Rate [Avg] (count/min)") or 0)
        hr_min = float(g("Heart Rate [Min] (count/min)") or 0)
        hr_max = float(g("Heart Rate [Max] (count/min)") or 0)
        steps  = g("Step Count (count)")
        active = g("Active Energy (kcal)")
        basal  = g("Resting Energy (kcal)")

        w_hour = WORKOUTS[d][1] if d in WORKOUTS else None

        # Heart rate spread
        if hr_avg and hr_min and hr_max:
            records += spread_hr(d, hr_min, hr_avg, hr_max, w_hour)

        # Steps — single daily record at noon
        if steps:
            records.append(rec("HKQuantityTypeIdentifierStepCount",
                               steps, "count", d, 12))

        # Active calories — mid afternoon
        if active:
            records.append(rec("HKQuantityTypeIdentifierActiveEnergyBurned",
                               active, "Cal", d, 15))

        # Basal calories — morning
        if basal:
            records.append(rec("HKQuantityTypeIdentifierBasalEnergyBurned",
                               basal, "Cal", d, 6))

        # Workout
        if d in WORKOUTS:
            wtype, whour, wdur, wcal = WORKOUTS[d]
            records.append(workout(d, wtype, whour, wdur, wcal))

print('<?xml version="1.0" encoding="UTF-8"?>')
print('<HealthData locale="en_US">')
for r in records:
    print(r)
print('</HealthData>')

print(f"\n<!-- {len(records)} records across {len(SELECTED)} days -->", file=sys.stderr)
print("Days: Jan 1 (high HR), Jan 29 (rest), Jan 31 (cycling), Feb 1 (run),", file=sys.stderr)
print("      Feb 2 (recovery), Feb 16 (intense cycling), Feb 17 (recovery)", file=sys.stderr)
