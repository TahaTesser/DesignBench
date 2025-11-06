package android

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tahatesser/designbench/pkg/report"
)

// Config controls a single Android render benchmark invocation.
type Config struct {
	Component          string
	Package            string
	Activity           string
	DeviceID           string
	ADBPath            string
	LaunchArgs         []string
	Timeout            time.Duration
	BenchmarkComponent string
}

// Run executes a basic render benchmark using `adb shell am start -W` to capture launch timings.
func Run(ctx context.Context, cfg Config) (*report.AndroidMetrics, error) {
	if cfg.Package == "" {
		return nil, errors.New("android package name is required")
	}
	if cfg.Activity == "" {
		return nil, errors.New("android activity is required")
	}

	component := cfg.Component
	if component == "" {
		component = cfg.Activity
	}

	adb := cfg.ADBPath
	if adb == "" {
		adb = "adb"
	}

	componentArg := buildComponentArg(cfg.Package, cfg.Activity)
	args := make([]string, 0, 8+len(cfg.LaunchArgs))
	if cfg.DeviceID != "" {
		args = append(args, "-s", cfg.DeviceID)
	}
	args = append(args, "shell", "am", "start", "-W", componentArg)
	if cfg.BenchmarkComponent != "" {
		args = append(args, "-e", "designbench_component", cfg.BenchmarkComponent)
	}
	args = append(args, cfg.LaunchArgs...)

	cmd := exec.CommandContext(ctx, adb, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run adb: %w: %s", err, stdout.String())
	}

	metrics := parseLaunchOutput(stdout.Bytes())
	metrics.Component = component
	metrics.Activity = cfg.Activity
	metrics.Package = cfg.Package
	metrics.BenchmarkComponent = cfg.BenchmarkComponent
	metrics.Command = fmt.Sprintf("%s %s", adb, strings.Join(args, " "))
	metrics.Timestamp = time.Now()
	metrics.Device = fetchDeviceMetadata(ctx, adb, cfg.DeviceID)
	if memoryMB, err := collectMemoryUsage(ctx, adb, cfg.DeviceID, cfg.Package); err == nil {
		metrics.MemoryMB = memoryMB
	}
	if cpuPercent, cpuTimeMs, err := collectCPUMetrics(ctx, adb, cfg.DeviceID, cfg.Package); err == nil {
		if cpuPercent > 0 {
			metrics.CPUPercent = cpuPercent
		}
		if cpuTimeMs > 0 {
			metrics.CPUTimeMs = cpuTimeMs
		}
	}

	return metrics, nil
}

func buildComponentArg(pkgName, activity string) string {
	if strings.Contains(activity, "/") {
		return activity
	}
	normalized := normalizeActivity(pkgName, activity)
	return fmt.Sprintf("%s/%s", pkgName, normalized)
}

func normalizeActivity(pkgName, activity string) string {
	if activity == "" {
		return activity
	}
	if strings.HasPrefix(activity, ".") {
		return activity
	}
	if pkgName != "" {
		prefix := pkgName + "."
		if strings.HasPrefix(activity, prefix) {
			return activity[len(pkgName):]
		}
	}
	return activity
}

func parseLaunchOutput(output []byte) *report.AndroidMetrics {
	result := &report.AndroidMetrics{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "ThisTime":
			if v, err := strconv.ParseFloat(value, 64); err == nil {
				result.FirstFrameMs = v
			}
		case "TotalTime":
			if v, err := strconv.ParseFloat(value, 64); err == nil {
				result.TotalTimeMs = v
			}
		case "WaitTime":
			if v, err := strconv.ParseFloat(value, 64); err == nil {
				result.WaitTimeMs = v
			}
		case "LaunchState":
			result.LaunchState = value
		}
	}
	return result
}

func fetchDeviceMetadata(ctx context.Context, adbPath, deviceID string) *report.DeviceMetadata {
	meta := &report.DeviceMetadata{
		ID:       deviceID,
		Platform: "android",
	}

	model, err := runADB(ctx, adbPath, deviceID, "shell", "getprop", "ro.product.model")
	if err == nil {
		meta.Model = strings.TrimSpace(model)
	}
	osVersion, err := runADB(ctx, adbPath, deviceID, "shell", "getprop", "ro.build.version.release")
	if err == nil {
		meta.OSVersion = strings.TrimSpace(osVersion)
	}
	resolution, err := runADB(ctx, adbPath, deviceID, "shell", "wm", "size")
	if err == nil {
		meta.Resolution = strings.TrimSpace(resolution)
	}
	if meta.Model == "" && meta.OSVersion == "" && meta.Resolution == "" && meta.ID == "" {
		return nil
	}
	return meta
}

func runADB(ctx context.Context, adbPath, deviceID string, args ...string) (string, error) {
	baseArgs := make([]string, 0, len(args)+2)
	if deviceID != "" {
		baseArgs = append(baseArgs, "-s", deviceID)
	}
	baseArgs = append(baseArgs, args...)
	cmd := exec.CommandContext(ctx, adbPath, baseArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func collectMemoryUsage(ctx context.Context, adbPath, deviceID, packageName string) (float64, error) {
	if packageName == "" {
		return 0, errors.New("package name required for memory collection")
	}
	out, err := runADB(ctx, adbPath, deviceID, "shell", "dumpsys", "meminfo", packageName)
	if err != nil {
		return 0, fmt.Errorf("dumpsys meminfo: %w", err)
	}
	return parseMeminfoForMB(out)
}

func parseMeminfoForMB(output string) (float64, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "TOTAL PSS") || strings.HasPrefix(upper, "TOTAL") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				line = line[idx+1:]
			}
			fields := strings.Fields(line)
			for _, field := range fields {
				clean := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(field, "kB"), "KB"), "kb")
				if v, err := strconv.ParseFloat(clean, 64); err == nil {
					return v / 1024.0, nil
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("unable to locate TOTAL memory usage in dumpsys output")
}

func collectCPUMetrics(ctx context.Context, adbPath, deviceID, packageName string) (float64, float64, error) {
	pid, err := resolveAndroidPID(ctx, adbPath, deviceID, packageName)
	if err != nil {
		return 0, 0, err
	}

	cpuPercent, percentErr := androidCPUPercent(ctx, adbPath, deviceID, pid, packageName)
	cpuTimeMs, timeErr := androidCPUTime(ctx, adbPath, deviceID, pid)

	if percentErr != nil && timeErr != nil {
		return 0, 0, fmt.Errorf("cpu metrics unavailable: %v; %v", percentErr, timeErr)
	}
	return cpuPercent, cpuTimeMs, nil
}

func resolveAndroidPID(ctx context.Context, adbPath, deviceID, packageName string) (string, error) {
	out, err := runADB(ctx, adbPath, deviceID, "shell", "pidof", packageName)
	if err == nil {
		pid := strings.TrimSpace(out)
		if pid != "" {
			return strings.Fields(pid)[0], nil
		}
	}

	psOut, psErr := runADB(ctx, adbPath, deviceID, "shell", "ps")
	if psErr != nil {
		if err != nil {
			return "", fmt.Errorf("pid lookup failed: %v; %v", err, psErr)
		}
		return "", psErr
	}
	scanner := bufio.NewScanner(strings.NewReader(psOut))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, packageName) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid := fields[0]
		if _, convErr := strconv.Atoi(pid); convErr == nil {
			return pid, nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", scanErr
	}
	return "", fmt.Errorf("process for %s not found", packageName)
}

func androidCPUPercent(ctx context.Context, adbPath, deviceID, pid, packageName string) (float64, error) {
	out, err := runADB(ctx, adbPath, deviceID, "shell", "top", "-b", "-n", "1", "-p", pid)
	if err == nil {
		if value, parseErr := parseAndroidTopCPU(out, pid); parseErr == nil {
			return value, nil
		}
	}
	cpuInfo, err := runADB(ctx, adbPath, deviceID, "shell", "dumpsys", "cpuinfo")
	if err != nil {
		return 0, err
	}
	return parseDumpsysCPU(cpuInfo, packageName)
}

func parseAndroidTopCPU(output, pid string) (float64, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] != pid {
			continue
		}
		for _, field := range fields[1:] {
			if strings.Contains(field, "%") {
				value := strings.TrimSuffix(field, "%")
				val, err := strconv.ParseFloat(value, 64)
				if err == nil {
					return val, nil
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("pid not present in top output")
}

func parseDumpsysCPU(output, packageName string) (float64, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, packageName) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		first := strings.TrimSuffix(fields[0], "%")
		if val, err := strconv.ParseFloat(first, 64); err == nil {
			return val, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("package not present in cpuinfo output")
}

const clockTicksPerSecond = 100.0

func androidCPUTime(ctx context.Context, adbPath, deviceID, pid string) (float64, error) {
	statPath := fmt.Sprintf("/proc/%s/stat", pid)
	out, err := runADB(ctx, adbPath, deviceID, "shell", "cat", statPath)
	if err != nil {
		return 0, fmt.Errorf("read proc stat: %w", err)
	}
	fields := strings.Fields(out)
	if len(fields) < 16 {
		return 0, errors.New("unexpected /proc stat format")
	}
	utime, err := strconv.ParseFloat(fields[13], 64)
	if err != nil {
		return 0, err
	}
	stime, err := strconv.ParseFloat(fields[14], 64)
	if err != nil {
		return 0, err
	}
	totalTicks := utime + stime
	return (totalTicks / clockTicksPerSecond) * 1000.0, nil
}
