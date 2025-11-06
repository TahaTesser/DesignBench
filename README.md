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
- `designbench preflight` — detect project metadata and connected devices.

Run Android and iOS sequentially by invoking each command:

```sh
designbench android --component ScreenX --package com.example.app --activity .BenchmarkActivity
designbench ios --component ScreenX --bundle com.example.app
```

## Reports

Results are written to `designbench-reports/` in JSON and echoed as a terminal summary. Use `--output` or `--reports-dir` to customize filenames and locations.
