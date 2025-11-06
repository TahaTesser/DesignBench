# DesignBench

DesignBench is a Go CLI that benchmarks initial UI render performance for Kotlin Multiplatform projects across Android (Compose) and iOS (SwiftUI).

## Install

```sh
go install github.com/tahatesser/designbench/cmd/designbench@latest
```

Ensure the install location (usually `$HOME/go/bin`) is on your `PATH`.

## Run Benchmarks

- `designbench android` — run Compose benchmarks via `adb`.
- `designbench ios` — run SwiftUI benchmarks via `xcodebuild`.
- `designbench all` — run both platforms with one command.
- `designbench preflight` — detect project metadata and connected devices.

Typical run:

```sh
designbench all \
  --component ScreenX \
  --android-package com.example.app \
  --android-activity .BenchmarkActivity \
  --android-device emulator-5554 \
  --ios-bundle com.example.app \
  --ios-device <simulator-udid>
```

## Reports

Results are written to `designbench-reports/` in JSON and echoed as a terminal summary. Use `--output` or `--reports-dir` to customize filenames and locations.

