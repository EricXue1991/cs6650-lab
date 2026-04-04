import argparse
import os
import pandas as pd
import matplotlib.pyplot as plt


def load_csv(path: str) -> pd.DataFrame:
    df = pd.read_csv(path)
    required = {
        "timestamp",
        "operation",
        "key",
        "latency_ms",
        "success",
        "http_status",
        "returned_version",
        "expected_version",
        "stale_read",
        "time_since_last_write_ms",
    }
    missing = required - set(df.columns)
    if missing:
        raise ValueError(f"{path} is missing columns: {missing}")
    return df


def save_histogram(series, title, xlabel, output_path, bins=30):
    plt.figure(figsize=(8, 5))
    plt.hist(series.dropna(), bins=bins)
    plt.title(title)
    plt.xlabel(xlabel)
    plt.ylabel("Count")
    plt.tight_layout()
    plt.savefig(output_path)
    plt.close()


def save_bar(labels, values, title, ylabel, output_path):
    plt.figure(figsize=(8, 5))
    plt.bar(labels, values)
    plt.title(title)
    plt.ylabel(ylabel)
    plt.tight_layout()
    plt.savefig(output_path)
    plt.close()


def summarize(df: pd.DataFrame) -> dict:
    reads = df[df["operation"] == "read"].copy()
    writes = df[df["operation"] == "write"].copy()

    summary = {
        "total_requests": len(df),
        "total_reads": len(reads),
        "total_writes": len(writes),
        "successful_reads": int(reads["success"].sum()),
        "successful_writes": int(writes["success"].sum()),
        "stale_reads": int(reads["stale_read"].sum()),
        "avg_read_latency_ms": reads["latency_ms"].mean() if len(reads) else 0,
        "avg_write_latency_ms": writes["latency_ms"].mean() if len(writes) else 0,
        "p95_read_latency_ms": reads["latency_ms"].quantile(0.95) if len(reads) else 0,
        "p95_write_latency_ms": writes["latency_ms"].quantile(0.95) if len(writes) else 0,
    }
    return summary


def write_summary(summary: dict, output_path: str):
    with open(output_path, "w") as f:
        for k, v in summary.items():
            f.write(f"{k}: {v}\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--csv", required=True, help="input CSV file")
    parser.add_argument("--outdir", required=True, help="output directory for plots")
    args = parser.parse_args()

    os.makedirs(args.outdir, exist_ok=True)

    df = load_csv(args.csv)
    summary = summarize(df)

    reads = df[df["operation"] == "read"].copy()
    writes = df[df["operation"] == "write"].copy()

    successful_reads = reads[reads["success"] == True]
    successful_writes = writes[writes["success"] == True]

    if len(successful_reads) > 0:
        save_histogram(
            successful_reads["latency_ms"],
            "Read Latency Distribution",
            "Latency (ms)",
            os.path.join(args.outdir, "read_latency_hist.png"),
        )

    if len(successful_writes) > 0:
        save_histogram(
            successful_writes["latency_ms"],
            "Write Latency Distribution",
            "Latency (ms)",
            os.path.join(args.outdir, "write_latency_hist.png"),
        )

    valid_intervals = reads[reads["time_since_last_write_ms"] >= 0]
    if len(valid_intervals) > 0:
        save_histogram(
            valid_intervals["time_since_last_write_ms"],
            "Time Since Last Write Distribution",
            "Time since last write (ms)",
            os.path.join(args.outdir, "time_since_last_write_hist.png"),
        )

    save_bar(
        ["fresh_reads", "stale_reads"],
        [
            int((reads["stale_read"] == False).sum()),
            int((reads["stale_read"] == True).sum()),
        ],
        "Fresh vs Stale Reads",
        "Count",
        os.path.join(args.outdir, "stale_read_bar.png"),
    )

    write_summary(summary, os.path.join(args.outdir, "summary.txt"))

    print(f"Plots and summary written to: {args.outdir}")
    print("Summary:")
    for k, v in summary.items():
        print(f"  {k}: {v}")


if __name__ == "__main__":
    main()