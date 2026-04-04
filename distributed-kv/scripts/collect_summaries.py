import os
import csv

def parse_summary_file(path):
    data = {}
    with open(path, "r") as f:
        for line in f:
            if ":" not in line:
                continue
            key, value = line.strip().split(":", 1)
            data[key.strip()] = value.strip()
    return data

def main():
    rows = []

    for name in os.listdir("."):
        if not name.startswith("plots_"):
            continue
        summary_path = os.path.join(name, "summary.txt")
        if not os.path.isfile(summary_path):
            continue

        summary = parse_summary_file(summary_path)
        summary["experiment"] = name.replace("plots_", "")
        rows.append(summary)

    rows.sort(key=lambda x: x["experiment"])

    fieldnames = [
        "experiment",
        "total_requests",
        "total_reads",
        "total_writes",
        "successful_reads",
        "successful_writes",
        "stale_reads",
        "avg_read_latency_ms",
        "avg_write_latency_ms",
        "p95_read_latency_ms",
        "p95_write_latency_ms",
    ]

    with open("all_results_summary.csv", "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for row in rows:
            writer.writerow({k: row.get(k, "") for k in fieldnames})

    print("Wrote all_results_summary.csv")

if __name__ == "__main__":
    main()