# AGENTS.md

## Overview

This document outlines the architecture, goals, and workflow for **DesignBench**, a **Go-based UI Performance Benchmarking CLI Tool** for Kotlin Multiplatform (KMP) codebases. DesignBench measures **static UI rendering performance** (initial render) and **system resource usage** (CPU, memory) across Android (Jetpack Compose) and iOS (SwiftUI) platforms. It supports both local and CI execution, with terminal summaries and JSON output for automated reporting.

---

## Goals

* Benchmark **Compose and SwiftUI** rendering in KMP projects.
* Measure **startup time**, **frame render times**, **CPU load**, and **memory usage**.
* Run benchmarks on **real devices or simulators**, with **consistent configurations**.
* Generate both **human-readable terminal summaries** and **machine-readable JSON reports**.

---

## Android Agent — Jetpack Compose

### Tools

* **Jetpack Macrobenchmark** (AndroidX Benchmark)
* **Jetpack Microbenchmark** (optional)
* **Perfetto / ADB / dumpsys** for system metrics

### Responsibilities

1. **Render Benchmarks:** Launch Compose screens or components and measure rendering duration.
2. **Metrics Captured:**

   * `StartupTimingMetric` – app launch times.
   * `FrameTimingMetric` – frame render durations (P50, P90, P95, P99).
   * Memory usage (`adb shell dumpsys meminfo`).
   * CPU usage (Perfetto traces or `adb shell top`).
3. **Execution:**

   * Use `Gradle` or `adb shell am instrument` commands.
   * Always run **release builds** on **physical devices**.
4. **Output:**

   * JSON report generated automatically (`androidx.benchmark.output.enable true`).
   * Parsed and summarized by DesignBench.

### Example Output

```json
{
  "component": "ScreenX",
  "android": {
    "firstFrameMs": 8,
    "frameTimeMs_P50": 10,
    "peakMemoryMB": 45
  }
}
```

---

## iOS Agent — SwiftUI

### Tools

* **XCTest Performance Testing APIs**
* **XCTMetric** suite (`XCTCPUMetric`, `XCTMemoryMetric`, etc.)
* **os_signpost** and **Instruments/xctrace** (optional, deeper profiling)

### Responsibilities

1. **Render Benchmarks:** Launch SwiftUI views via XCTest or UIHostingController.
2. **Metrics Captured:**

   * CPU usage via `XCTCPUMetric`
   * Memory usage via `XCTMemoryMetric`
   * Render duration via `XCTClockMetric`
3. **Execution:**

   * Run via `xcodebuild` on macOS.
   * Prefer **real devices**, fallback to simulators for CI.
4. **Output:**

   * Extract results with `xcresulttool` or `XCMetrics`.
   * Export JSON summaries.

### Example Output

```json
{
  "component": "ScreenX",
  "ios": {
    "renderTimeMs_avg": 120,
    "memoryMB": 50,
    "cpuTimeSec": 0.2
  }
}
```

---

## CLI Orchestration (Cross-Platform Agent)

### Responsibilities

* Unified entrypoint for both platforms.
* Commands:

  * `designbench android`
  * `designbench ios`
  * `designbench all`
* Handles build, execution, result collection, and aggregation.
* Annotates results with device metadata (model, OS version, screen resolution).

### Implementation

* Implemented in **Go (Golang)** using modular packages:

  * `cmd/` — CLI entrypoints.
  * `pkg/android` — Android benchmark orchestration.
  * `pkg/ios` — iOS benchmark orchestration.
  * `pkg/report` — JSON aggregation, schema validation, and pretty terminal rendering.
* Executes `adb` and `xcodebuild` commands through Go’s `os/exec` package.
* Aggregates results into combined JSON and terminal summaries.

---

## CI Integration Agent

### Setup

* Run iOS benchmarks on **macOS runners**.
* Run Android benchmarks on **macOS/Linux runners**.
* Optionally use **Firebase Test Lab** or **BrowserStack** for physical devices.

### Workflow

1. Build benchmark targets.
2. Run via DesignBench commands.
3. Collect JSON outputs as CI artifacts.
4. Merge reports and detect regressions.

### Regression Detection

* Define thresholds (e.g., render < 50 ms, memory < 100 MB).
* Compare metrics against baselines.
* Fail CI on performance regressions (non-zero exit).

---

## Output & Reporting

* **Terminal Summary:** concise, color-coded results.
* **JSON Reports:** stored for CI and historical trend analysis.
* **Optional Dashboard Integration:** push data to Grafana or custom dashboards.

---

## Inspirations & References

* **Flutter Integration Tests** — timeline & frame metrics JSON outputs.
* **Apptim** — mobile performance profiling model.
* **Android Macrobenchmark & XCTest** — native performance frameworks.

---

## Future Extensions

* Add **animation and scroll benchmarking**.
* Include **GPU and battery metrics**.
* Integrate with **KMP design system catalog** to benchmark every component variant.

---

> **DesignBench** provides a unified, automated, and extensible foundation for benchmarking UI performance across Android and iOS in a Kotlin Multiplatform project, powered by Go for robust orchestration and cross-platform automation.
