package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DeviceMetadata captures basic information about the device that produced a benchmark.
type DeviceMetadata struct {
	ID         string `json:"id,omitempty"`
	Model      string `json:"model,omitempty"`
	OSVersion  string `json:"osVersion,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Resolution string `json:"resolution,omitempty"`
}

// AndroidMetrics represents render/startup timing measurements collected from an Android device.
type AndroidMetrics struct {
	Component          string          `json:"component"`
	Activity           string          `json:"activity"`
	Package            string          `json:"package"`
	BenchmarkComponent string          `json:"benchmarkComponent,omitempty"`
	FirstFrameMs       float64         `json:"firstFrameMs,omitempty"`
	TotalTimeMs        float64         `json:"totalTimeMs,omitempty"`
	WaitTimeMs         float64         `json:"waitTimeMs,omitempty"`
	MemoryMB           float64         `json:"memoryMb,omitempty"`
	CPUPercent         float64         `json:"cpuPercent,omitempty"`
	CPUTimeMs          float64         `json:"cpuTimeMs,omitempty"`
	LaunchState        string          `json:"launchState,omitempty"`
	Device             *DeviceMetadata `json:"device,omitempty"`
	Command            string          `json:"command,omitempty"`
	Timestamp          time.Time       `json:"timestamp"`
}

// IOSMetrics represents render/startup measurements captured from an iOS simulator/device.
type IOSMetrics struct {
	Component          string          `json:"component"`
	BundleID           string          `json:"bundleId"`
	LaunchArgs         []string        `json:"launchArgs,omitempty"`
	BenchmarkComponent string          `json:"benchmarkComponent,omitempty"`
	RenderTimeMs       float64         `json:"renderTimeMs,omitempty"`
	MemoryMB           float64         `json:"memoryMb,omitempty"`
	CPUPercent         float64         `json:"cpuPercent,omitempty"`
	CPUTimeMs          float64         `json:"cpuTimeMs,omitempty"`
	Device             *DeviceMetadata `json:"device,omitempty"`
	Command            string          `json:"command,omitempty"`
	Timestamp          time.Time       `json:"timestamp"`
}

// Result aggregates metrics for a single component across supported platforms.
type Result struct {
	Component  string          `json:"component"`
	Android    *AndroidMetrics `json:"android,omitempty"`
	IOS        *IOSMetrics     `json:"ios,omitempty"`
	CLICommand string          `json:"cliCommand,omitempty"`
}

// SaveJSON writes the aggregated result to the provided file path.
func SaveJSON(path string, result Result) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}

// FormatSummary returns a concise, human-readable summary for terminal output.
func FormatSummary(res Result) string {
	out := fmt.Sprintf("Component: %s\n", res.Component)
	if res.Android != nil {
		model := "-"
		if res.Android.Device != nil && res.Android.Device.Model != "" {
			model = res.Android.Device.Model
		}
		mem := "-"
		if res.Android.MemoryMB > 0 {
			mem = fmt.Sprintf("%.1fMB", res.Android.MemoryMB)
		}
		cpu := "-"
		if res.Android.CPUPercent > 0 {
			cpu = fmt.Sprintf("%.1f%%", res.Android.CPUPercent)
		}
		cpuTime := "-"
		if res.Android.CPUTimeMs > 0 {
			cpuTime = fmt.Sprintf("%.0fms", res.Android.CPUTimeMs)
		}
		out += fmt.Sprintf("  Android[%s]: total=%.1fms firstFrame=%.1fms wait=%.1fms memory=%s cpu=%s cpuTime=%s\n",
			model,
			res.Android.TotalTimeMs,
			res.Android.FirstFrameMs,
			res.Android.WaitTimeMs,
			mem,
			cpu,
			cpuTime)
	}
	if res.IOS != nil {
		model := "-"
		if res.IOS.Device != nil && res.IOS.Device.Model != "" {
			model = res.IOS.Device.Model
		}
		mem := "-"
		if res.IOS.MemoryMB > 0 {
			mem = fmt.Sprintf("%.1fMB", res.IOS.MemoryMB)
		}
		cpu := "-"
		if res.IOS.CPUPercent > 0 {
			cpu = fmt.Sprintf("%.1f%%", res.IOS.CPUPercent)
		}
		cpuTime := "-"
		if res.IOS.CPUTimeMs > 0 {
			cpuTime = fmt.Sprintf("%.0fms", res.IOS.CPUTimeMs)
		}
		out += fmt.Sprintf("  iOS[%s]: render=%.1fms memory=%s cpu=%s cpuTime=%s\n",
			model,
			res.IOS.RenderTimeMs,
			mem,
			cpu,
			cpuTime)
	}
	return out
}
