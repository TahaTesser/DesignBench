# DesignBench

DesignBench is a Go-based CLI for orchestrating cross-platform UI performance benchmarks in Kotlin Multiplatform projects. The first milestone implements basic render time launches for Android and iOS so you can validate the pipeline end-to-end before integrating deeper measurement harnesses.

## Installation

Install from source via Go:

```sh
go install github.com/tahatesser/designbench/cmd/designbench@latest
```

Ensure `$GOBIN` (or `$GOPATH/bin`, defaulting to `$HOME/go/bin`) is on your `$PATH`, then invoke `designbench` from any directory.

Prefer a fixed binary or building locally? Run:

```sh
go build -o /usr/local/bin/designbench ./cmd/designbench
```

Swap `/usr/local/bin` for any directory already in your `$PATH`.

## Commands

- `designbench android` launches a Compose activity through `adb shell am start -W` and captures startup / first-frame timing plus device metadata.
- `designbench ios` launches a SwiftUI host bundle through `xcrun simctl launch` and records launch timing for the selected simulator or device.
- `designbench all` runs both benchmarks sequentially and aggregates the results into one summary.
- `designbench preflight` inspects the current project and any booted devices to suggest ready-to-run commands.

All commands share `--component` to label the run, `--output` (optional) to override the JSON report name, `--reports-dir` (default `designbench-reports/`) to control where files land, and `--timeout` to bound execution. When `--output` is omitted the CLI writes reports like `designbench-reports/<component>-<platform>.json`.

## Android usage

```sh
go run ./cmd/designbench android \
  --component ScreenX \
  --package com.example.app \
  --activity .BenchmarkActivity \
  --benchmark-component MyComposable \
  --device emulator-5554
```

By default the report lands at `designbench-reports/screenx-android.json`; pass `--output <name>.json` to override. The JSON records the exact `designbench` command alongside device metrics for auditing.

```
Component: ScreenX
  Android[Pixel 7]: total=620.0ms firstFrame=180.0ms wait=650.0ms
```

## iOS usage

```sh
go run ./cmd/designbench ios \
  --component ScreenX \
  --bundle com.example.app \
  --benchmark-component DetailView \
  --device <simulator-udid>
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
  --ios-device <simulator-udid>
```

The resulting JSON report mirrors the structure outlined in `AGENTS.md` and is ready for CI ingestion. It defaults to `designbench-reports/screenx-all.json` unless you override `--output`.

## Preflight helper

Run `designbench preflight` at the project root to auto-discover manifest/plist metadata and any connected devices:

```sh
go run ./cmd/designbench preflight
```

Example output:

```
Preflight (root: /path/to/project)
 
Android:
  Package: com.example.app
  Activity: .BenchmarkActivity
  Manifest: androidApp/src/main/AndroidManifest.xml
  Device: 123456F (Pixel_7_API_34)

iOS:
  Bundle: com.example.app
  Info.plist: iosApp/iosApp/Info.plist
  Device: 41C... (iPhone 15 Pro)
  Runtime: com.apple.CoreSimulator.SimRuntime.iOS-17-5

Suggested commands:
  Android:
    designbench android -p com.example.app -a .BenchmarkActivity --device 123456F

  iOS:
    designbench ios -b com.example.app --device 41C...

  Cross-platform:
    designbench all --android-package com.example.app --android-activity .BenchmarkActivity --android-device 123456F --ios-bundle com.example.app --ios-device 41C...
```

Use the suggested commands as-is or tweak flags to suit your benchmark environment.

If metadata is missing, placeholders such as `<android-package>` or `<ios-bundle>` are shown so you know which flags to fill in manually.

## Next steps

- Replace the launch-based probes with dedicated Macrobenchmark and XCTest performance harnesses.
- Extend metrics to include CPU, memory, and frame distribution.
- Add regression thresholds and CI wiring to fail builds on performance drift.
