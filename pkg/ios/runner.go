package ios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
