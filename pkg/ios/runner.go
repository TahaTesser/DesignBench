package ios

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tahatesser/designbench/pkg/report"
)

// Config controls an iOS render benchmark invocation.
type Config struct {
	Component          string
	BundleID           string
	DeviceID           string
	LaunchArgs         []string
	XCRunPath          string
	BenchmarkComponent string
}

// Run executes a simple launch benchmark by invoking `xcrun simctl launch` and timing its duration.
func Run(ctx context.Context, cfg Config) (*report.IOSMetrics, error) {
	if cfg.BundleID == "" {
		return nil, errors.New("ios bundle id is required")
	}

	xcrun := cfg.XCRunPath
	if xcrun == "" {
		xcrun = "xcrun"
	}

	component := cfg.Component
	if component == "" {
		component = cfg.BundleID
	}

	deviceMetadata, err := resolveDeviceMetadata(ctx, xcrun, cfg.DeviceID)
	if err != nil {
		return nil, err
	}
	deviceID := deviceMetadata.ID
	if deviceID == "" {
		return nil, errors.New("no booted simulator found; provide --device to target a specific simulator or device")
	}

	args := append([]string{"simctl", "launch", deviceID, cfg.BundleID}, cfg.LaunchArgs...)
	cmd := exec.CommandContext(ctx, xcrun, args...)
	if cfg.BenchmarkComponent != "" {
		env := append(os.Environ(), "SIMCTL_CHILD_DESIGNBENCH_COMPONENT="+cfg.BenchmarkComponent)
		cmd.Env = env
	}
	start := time.Now()
	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("run xcrun: %w: %s", err, string(output))
	}

	metrics := &report.IOSMetrics{
		Component:          component,
		BundleID:           cfg.BundleID,
		LaunchArgs:         cfg.LaunchArgs,
		BenchmarkComponent: cfg.BenchmarkComponent,
		RenderTimeMs:       float64(elapsed) / float64(time.Millisecond),
		Command:            fmt.Sprintf("%s %s", xcrun, strings.Join(args, " ")),
		Timestamp:          time.Now(),
		Device:             deviceMetadata,
	}

	if memoryMB, err := collectMemoryUsage(ctx, xcrun, deviceID, cfg.BundleID); err == nil {
		metrics.MemoryMB = memoryMB
	}
	if cpuPercent, cpuTimeMs, err := collectIOSCPUMetrics(ctx, xcrun, deviceID, cfg.BundleID); err == nil {
		if cpuPercent > 0 {
			metrics.CPUPercent = cpuPercent
		}
		if cpuTimeMs > 0 {
			metrics.CPUTimeMs = cpuTimeMs
		}
	}

	return metrics, nil
}

type simctlDevice struct {
	UDID                 string `json:"udid"`
	Name                 string `json:"name"`
	State                string `json:"state"`
	DeviceTypeIdentifier string `json:"deviceTypeIdentifier"`
	Runtime              string `json:"runtime"`
	IsAvailable          bool   `json:"isAvailable"`
	AvailabilityError    string `json:"availabilityError"`
}

type simctlList struct {
	Devices map[string][]simctlDevice `json:"devices"`
}

func resolveDeviceMetadata(ctx context.Context, xcrunPath, requestedID string) (*report.DeviceMetadata, error) {
	devices, err := listSimctlDevices(ctx, xcrunPath)
	if err != nil && requestedID == "" {
		return &report.DeviceMetadata{Platform: "ios"}, nil
	}
	if err != nil {
		return nil, err
	}

	if requestedID != "" {
		if dev, ok := devices[requestedID]; ok {
			return simctlToMetadata(dev), nil
		}
		// fallback to minimal metadata if device not in simulator list (likely physical)
		return &report.DeviceMetadata{
			ID:       requestedID,
			Platform: "ios",
		}, nil
	}

	for _, dev := range devices {
		if strings.EqualFold(dev.State, "Booted") {
			return simctlToMetadata(dev), nil
		}
	}

	return &report.DeviceMetadata{Platform: "ios"}, nil
}

func listSimctlDevices(ctx context.Context, xcrunPath string) (map[string]simctlDevice, error) {
	cmd := exec.CommandContext(ctx, xcrunPath, "simctl", "list", "devices", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list simulators: %w: %s", err, string(out))
	}
	var payload simctlList
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("decode simctl json: %w", err)
	}
	result := make(map[string]simctlDevice)
	for runtime, devices := range payload.Devices {
		for _, device := range devices {
			device.Runtime = runtime
			result[device.UDID] = device
		}
	}
	return result, nil
}

func simctlToMetadata(device simctlDevice) *report.DeviceMetadata {
	meta := &report.DeviceMetadata{
		ID:       device.UDID,
		Model:    device.Name,
		Platform: "ios",
	}
	if device.Runtime != "" {
		meta.OSVersion = runtimeToVersion(device.Runtime)
	}
	if device.DeviceTypeIdentifier != "" {
		meta.Resolution = device.DeviceTypeIdentifier
	}
	return meta
}

func runtimeToVersion(runtime string) string {
	const prefix = "com.apple.CoreSimulator.SimRuntime."
	if strings.HasPrefix(runtime, prefix) {
		runtime = runtime[len(prefix):]
	}
	runtime = strings.ReplaceAll(runtime, "_", "-")
	parts := strings.Split(runtime, "-")
	if len(parts) < 2 {
		return strings.TrimSpace(runtime)
	}
	name := parts[0]
	version := strings.Join(parts[1:], ".")
	switch strings.ToLower(name) {
	case "ios":
		name = "iOS"
	case "ipados":
		name = "iPadOS"
	case "tvos":
		name = "tvOS"
	default:
		lower := strings.ToLower(name)
		if len(lower) > 0 {
			name = strings.ToUpper(lower[:1]) + lower[1:]
		} else {
			name = lower
		}
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", name, version))
}

var sizePattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(bytes|b|kb|kib|mb|mib|gb|gib)`)

func collectMemoryUsage(ctx context.Context, xcrunPath, deviceID, bundleID string) (float64, error) {
	target := deviceID
	if target == "" {
		target = "booted"
	}
	if bundleID == "" {
		return 0, errors.New("bundle id required for memory collection")
	}
	args := []string{"simctl", "spawn", target, "memory_usage", "-b", bundleID}
	cmd := exec.CommandContext(ctx, xcrunPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("memory_usage: %w: %s", err, string(out))
	}
	return parseIOSMemoryOutput(out)
}

func parseIOSMemoryOutput(output []byte) (float64, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "physical") && strings.Contains(lower, "footprint") {
			if mb, err := parseSizeToMB(line); err == nil {
				return mb, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("physical footprint not found")
}

func parseSizeToMB(line string) (float64, error) {
	matches := sizePattern.FindStringSubmatch(line)
	if len(matches) < 3 {
		return 0, errors.New("size pattern not found")
	}
	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, err
	}
	unit := strings.ToLower(matches[2])
	switch unit {
	case "bytes", "b":
		return value / (1024.0 * 1024.0), nil
	case "kb", "kib":
		return value / 1024.0, nil
	case "mb", "mib":
		return value, nil
	case "gb", "gib":
		return value * 1024.0, nil
	default:
		return 0, fmt.Errorf("unknown unit %q", unit)
	}
}

func collectIOSCPUMetrics(ctx context.Context, xcrunPath, deviceID, bundleID string) (float64, float64, error) {
	pid, err := resolveIOSPID(ctx, xcrunPath, deviceID, bundleID)
	if err != nil {
		return 0, 0, err
	}
	percent, timeMs, metricsErr := iosProcessMetrics(ctx, xcrunPath, deviceID, pid)
	if metricsErr != nil {
		return 0, 0, metricsErr
	}
	return percent, timeMs, nil
}

func resolveIOSPID(ctx context.Context, xcrunPath, deviceID, bundleID string) (string, error) {
	target := deviceID
	if target == "" {
		target = "booted"
	}
	out, err := exec.CommandContext(ctx, xcrunPath, "simctl", "spawn", target, "launchctl", "list").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("launchctl list: %w: %s", err, string(out))
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		label := fields[len(fields)-1]
		if !strings.Contains(label, bundleID) {
			continue
		}
		pid := fields[0]
		if pid == "-" {
			continue
		}
		if _, err := strconv.Atoi(pid); err == nil {
			return pid, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("process pid not found via launchctl")
}

func iosProcessMetrics(ctx context.Context, xcrunPath, deviceID, pid string) (float64, float64, error) {
	target := deviceID
	if target == "" {
		target = "booted"
	}
	out, err := exec.CommandContext(ctx, xcrunPath, "simctl", "spawn", target, "ps", "-o", "pid,pcpu,time", "-p", pid).CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ps metrics: %w: %s", err, string(out))
	}
	return parseIOSPSMetrics(out, pid)
}

func parseIOSPSMetrics(output []byte, pid string) (float64, float64, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	parsedHeader := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !parsedHeader {
			parsedHeader = true
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != pid {
			continue
		}
		cpuPercent, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, 0, err
		}
		ms, err := parseDarwinTime(fields[2])
		if err != nil {
			return 0, 0, err
		}
		return cpuPercent, ms, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return 0, 0, errors.New("cpu metrics not found in ps output")
}

func parseDarwinTime(value string) (float64, error) {
	core := value
	var daySeconds float64
	if strings.Contains(value, "-") {
		dayParts := strings.SplitN(value, "-", 2)
		if len(dayParts) != 2 {
			return 0, errors.New("invalid day time format")
		}
		days, err := strconv.ParseFloat(dayParts[0], 64)
		if err != nil {
			return 0, err
		}
		daySeconds = days * 24 * 3600
		core = dayParts[1]
	}
	parts := strings.Split(core, ":")
	if len(parts) == 0 {
		return 0, errors.New("invalid time format")
	}
	// TIME column may appear as MM:SS or HH:MM:SS
	var ms float64
	switch len(parts) {
	case 2:
		// MM:SS
		minutes, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, err
		}
		seconds, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, err
		}
		ms = (minutes*60 + seconds) * 1000.0
	case 3:
		hours, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, err
		}
		minutes, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, err
		}
		seconds, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return 0, err
		}
		ms = ((hours * 3600) + (minutes * 60) + seconds) * 1000.0
	default:
		return 0, errors.New("unsupported time format")
	}
	return ms + (daySeconds * 1000.0), nil
}
