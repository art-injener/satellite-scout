#!/usr/bin/env python3
"""Эталонный калькулятор пролётов спутников на базе skyfield.

Используется для кросс-валидации расчётов satellite-scout.
Принимает JSON с параметрами (TLE, наблюдатель, время),
вычисляет пролёты через skyfield, записывает результат в JSON.

Использование:
    python calculator.py /data/input.json /data/output.json
"""

import json
import sys
from datetime import datetime, timezone

from skyfield.api import EarthSatellite, load, wgs84


def compute_passes(case_data):
    """Вычисляет пролёты спутника для одного тестового кейса."""
    ts = load.timescale()

    sat = EarthSatellite(
        case_data["tle_line1"],
        case_data["tle_line2"],
        case_data["tle_name"],
        ts,
    )

    observer = wgs84.latlon(
        case_data["observer_lat"],
        case_data["observer_lon"],
        elevation_m=case_data["observer_alt_km"] * 1000,
    )

    t0 = ts.from_datetime(
        datetime.fromtimestamp(case_data["start_unix"], tz=timezone.utc)
    )
    t1 = ts.from_datetime(
        datetime.fromtimestamp(case_data["end_unix"], tz=timezone.utc)
    )

    min_el = case_data["min_elevation_deg"]

    times, events = sat.find_events(observer, t0, t1, altitude_degrees=min_el)

    passes = []
    current = {}
    diff = sat - observer

    for ti, event in zip(times, events):
        topocentric = diff.at(ti)
        alt, az, distance = topocentric.altaz()
        unix_ms = int(ti.utc_datetime().replace(tzinfo=timezone.utc).timestamp() * 1000)

        if event == 0:  # rise (AOS)
            current = {
                "aos_unix_ms": unix_ms,
                "aos_az": round(az.degrees, 1),
            }
        elif event == 1:  # culmination (TCA)
            current["tca_unix_ms"] = unix_ms
            current["tca_el"] = round(alt.degrees, 1)
            current["tca_az"] = round(az.degrees, 1)
        elif event == 2:  # set (LOS)
            current["los_unix_ms"] = unix_ms
            current["los_az"] = round(az.degrees, 1)

            if "aos_unix_ms" in current and "tca_unix_ms" in current:
                current["duration_sec"] = round(
                    (current["los_unix_ms"] - current["aos_unix_ms"]) / 1000.0, 1
                )
                passes.append(current)
            current = {}

    return passes


def main():
    if len(sys.argv) != 3:
        print(f"Использование: {sys.argv[0]} <input.json> <output.json>", file=sys.stderr)
        sys.exit(1)

    input_path = sys.argv[1]
    output_path = sys.argv[2]

    with open(input_path) as f:
        cases = json.load(f)

    results = []
    for case_data in cases:
        try:
            passes = compute_passes(case_data)
            results.append({
                "name": case_data["name"],
                "passes": passes,
                "error": None,
            })
        except Exception as e:
            results.append({
                "name": case_data["name"],
                "passes": [],
                "error": str(e),
            })

    with open(output_path, "w") as f:
        json.dump(results, f, indent=2)

    print(f"Рассчитано {len(results)} кейсов", file=sys.stderr)


if __name__ == "__main__":
    main()
