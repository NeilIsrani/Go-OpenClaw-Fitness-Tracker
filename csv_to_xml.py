"""
Convert healthreport.csv rows (2026-01-31 to 2026-02-02) to Apple Health XML.
Usage: python3 csv_to_xml.py > real_health.xml
"""

import csv
from datetime import datetime, date

CSV_FILE = "healthreport.csv"
START_DATE = date(2026, 1, 31)
END_DATE   = date(2026, 2, 2)
SOURCE     = "Apple Watch"
DATE_FMT   = "%Y-%m-%d %H:%M:%S +0000"

# CSV column → (HK type, unit)
METRIC_MAP = {
    "Heart Rate [Avg] (count/min)":  ("HKQuantityTypeIdentifierHeartRate",           "count/min"),
    "Heart Rate [Min] (count/min)":  ("HKQuantityTypeIdentifierHeartRate",           "count/min"),
    "Heart Rate [Max] (count/min)":  ("HKQuantityTypeIdentifierHeartRate",           "count/min"),
    "Resting Heart Rate (count/min)":("HKQuantityTypeIdentifierHeartRate",           "count/min"),
    "Step Count (count)":            ("HKQuantityTypeIdentifierStepCount",           "count"),
    "Active Energy (kcal)":          ("HKQuantityTypeIdentifierActiveEnergyBurned",  "Cal"),
    "Resting Energy (kcal)":         ("HKQuantityTypeIdentifierBasalEnergyBurned",   "Cal"),
}

records = []

with open(CSV_FILE, newline="", encoding="utf-8") as f:
    reader = csv.DictReader(f)
    for row in reader:
        raw_date = row["Date/Time"].strip()
        try:
            row_date = datetime.strptime(raw_date, "%Y-%m-%d %H:%M:%S").date()
        except ValueError:
            continue

        if not (START_DATE <= row_date <= END_DATE):
            continue

        ts = datetime.strptime(raw_date, "%Y-%m-%d %H:%M:%S").strftime(DATE_FMT)

        for col, (hk_type, unit) in METRIC_MAP.items():
            val = row.get(col, "").strip()
            if not val:
                continue
            try:
                float(val)
            except ValueError:
                continue
            records.append(
                f'  <Record type="{hk_type}" value="{val}" unit="{unit}" '
                f'startDate="{ts}" sourceName="{SOURCE}"/>'
            )

print('<?xml version="1.0" encoding="UTF-8"?>')
print('<HealthData locale="en_US">')
for r in records:
    print(r)
print('</HealthData>')

import sys
print(f"\n<!-- {len(records)} records from {START_DATE} to {END_DATE} -->", file=sys.stderr)
