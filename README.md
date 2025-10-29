# DesignBench

DesignBench is a Go-based CLI for orchestrating cross-platform UI performance benchmarks in Kotlin Multiplatform projects. The first milestone implements basic render time launches for Android and iOS so you can validate the pipeline end-to-end before integrating deeper measurement harnesses.

## Commands

- `designbench android` launches a Compose activity through `adb shell am start -W` and captures startup / first-frame timing plus device metadata.
- `designbench ios` launches a SwiftUI host bundle through `xcrun simctl launch` and records launch timing for the selected simulator or device.
- `designbench all` runs both benchmarks sequentially and aggregates the results into one summary.

All commands share `--component` to label the run, `--output` to persist a JSON report, `--reports-dir` (default `reports/`) to control where files land, and `--timeout` to bound execution.

## Android usage

```sh
go run ./cmd/designbench android \
  --component ScreenX \
  --package com.example.app \
  --activity .BenchmarkActivity \
  --device emulator-5554 \
  --output android.json
```

The report is written to `reports/android.json` unless you pass an absolute path, and the JSON now records the exact `designbench` command alongside device metrics for auditing.

```
Component: ScreenX
  Android[Pixel 7]: total=620.0ms firstFrame=180.0ms wait=650.0ms
```

## iOS usage

```sh
go run ./cmd/designbench ios \
  --component ScreenX \
  --bundle com.example.app \
  --device <simulator-udid> \
  --output ios.json
```

When no `--device` is supplied, the CLI targets the first booted simulator discovered via `xcrun simctl list --json`.

## Combined run

```sh
go run ./cmd/designbench all \
  --component ScreenX \
  --android-package com.example.app \
  --android-activity .BenchmarkActivity \
  --ios-bundle com.example.app \
  --android-device emulator-5554 \
  --ios-device <simulator-udid> \
  --output combined.json
```

The resulting JSON report mirrors the structure outlined in `AGENTS.md` and is ready for CI ingestion.

## Next steps

- Replace the launch-based probes with dedicated Macrobenchmark and XCTest performance harnesses.
- Extend metrics to include CPU, memory, and frame distribution.
- Add regression thresholds and CI wiring to fail builds on performance drift.
