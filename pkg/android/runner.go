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
	Component  string
	Package    string
	Activity   string
	DeviceID   string
	ADBPath    string
	LaunchArgs []string
	Timeout    time.Duration
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
	metrics.Command = fmt.Sprintf("%s %s", adb, strings.Join(args, " "))
	metrics.Timestamp = time.Now()
	metrics.Device = fetchDeviceMetadata(ctx, adb, cfg.DeviceID)

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
