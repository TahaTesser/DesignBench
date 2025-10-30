package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tahatesser/designbench/pkg/android"
	"github.com/tahatesser/designbench/pkg/ios"
	"github.com/tahatesser/designbench/pkg/preflight"
	"github.com/tahatesser/designbench/pkg/report"
)

var (
	componentFlag string
	outputPath    string
	timeoutFlag   string
	reportsDir    string
)

const defaultReportsDir = "designbench-reports"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "designbench: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "designbench",
		Short: "DesignBench benchmarks UI render performance across Android and iOS.",
	}

	cmd.PersistentFlags().StringVar(&componentFlag, "component", "", "Component name label for the benchmark run.")
	cmd.PersistentFlags().StringVarP(&outputPath, "output", "o", "", "Override JSON output file name (defaults to auto-generated under reports-dir).")
	cmd.PersistentFlags().StringVar(&timeoutFlag, "timeout", "60s", "Overall command timeout (e.g. 45s, 2m).")
	cmd.PersistentFlags().StringVar(&reportsDir, "reports-dir", defaultReportsDir, "Directory where JSON reports are written.")

	cmd.AddCommand(newAndroidCmd(), newIOSCmd(), newAllCmd(), newPreflightCmd())

	return cmd
}

type androidOptions struct {
	packageName        string
	activity           string
	deviceID           string
	adbPath            string
	launchArgs         []string
	benchmarkComponent string
}

func (opts *androidOptions) bind(cmd *cobra.Command, prefix string) {
	nameFlag := prefix + "package"
	cmd.Flags().StringVarP(&opts.packageName, nameFlag, "p", "", "Android application package name.")
	cmd.MarkFlagRequired(nameFlag) // nolint:errcheck

	activityFlag := prefix + "activity"
	cmd.Flags().StringVarP(&opts.activity, activityFlag, "a", "", "Android activity to launch (e.g. .MainActivity).")
	cmd.MarkFlagRequired(activityFlag) // nolint:errcheck

	deviceFlag := prefix + "device"
	cmd.Flags().StringVar(&opts.deviceID, deviceFlag, "", "Target Android device serial (defaults to adb default).")

	adbFlag := prefix + "adb"
	cmd.Flags().StringVar(&opts.adbPath, adbFlag, "adb", "Path to the adb executable.")

	launchArgsFlag := prefix + "launch-args"
	cmd.Flags().StringSliceVar(&opts.launchArgs, launchArgsFlag, nil, "Additional arguments forwarded to `am start`.")

	benchmarkComponentFlag := prefix + "benchmark-component"
	cmd.Flags().StringVar(&opts.benchmarkComponent, benchmarkComponentFlag, "", "Compose component identifier to benchmark (forwarded as an extra to `am start`).")
}

type iosOptions struct {
	bundleID           string
	deviceID           string
	xcrunPath          string
	launchArgs         []string
	benchmarkComponent string
}

func (opts *iosOptions) bind(cmd *cobra.Command, prefix string) {
	bundleFlag := prefix + "bundle"
	cmd.Flags().StringVarP(&opts.bundleID, bundleFlag, "b", "", "iOS bundle identifier to launch.")
	cmd.MarkFlagRequired(bundleFlag) // nolint:errcheck

	deviceFlag := prefix + "device"
	cmd.Flags().StringVar(&opts.deviceID, deviceFlag, "", "Simulator/Device UDID. Defaults to first booted simulator.")

	xcrunFlag := prefix + "xcrun"
	cmd.Flags().StringVar(&opts.xcrunPath, xcrunFlag, "xcrun", "Path to the xcrun executable.")

	launchArgsFlag := prefix + "launch-args"
	cmd.Flags().StringSliceVar(&opts.launchArgs, launchArgsFlag, nil, "Extra arguments forwarded to `simctl launch`.")

	benchmarkComponentFlag := prefix + "benchmark-component"
	cmd.Flags().StringVar(&opts.benchmarkComponent, benchmarkComponentFlag, "", "SwiftUI component identifier to benchmark (forwarded via simulator environment).")
}

func newAndroidCmd() *cobra.Command {
	var opts androidOptions
	cmd := &cobra.Command{
		Use:   "android",
		Short: "Run Android render benchmark.",
		RunE: func(cmd *cobra.Command, args []string) error {
			component := resolveComponent(opts.activity)
			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			cfg := android.Config{
				Component:          component,
				Package:            opts.packageName,
				Activity:           opts.activity,
				DeviceID:           opts.deviceID,
				ADBPath:            opts.adbPath,
				LaunchArgs:         opts.launchArgs,
				BenchmarkComponent: opts.benchmarkComponent,
			}
			metrics, err := android.Run(ctx, cfg)
			if err != nil {
				return err
			}

			result := report.Result{
				Component:  component,
				Android:    metrics,
				CLICommand: currentCLICommand(cmd),
			}
			fmt.Print(report.FormatSummary(result))
			if path, err := resolveOutputFile(component, "android"); err != nil {
				return err
			} else if path != "" {
				if err := report.SaveJSON(path, result); err != nil {
					return err
				}
			}
			return nil
		},
	}
	opts.bind(cmd, "")
	return cmd
}

func newIOSCmd() *cobra.Command {
	var opts iosOptions
	cmd := &cobra.Command{
		Use:   "ios",
		Short: "Run iOS render benchmark.",
		RunE: func(cmd *cobra.Command, args []string) error {
			component := resolveComponent(opts.bundleID)
			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			cfg := ios.Config{
				Component:          component,
				BundleID:           opts.bundleID,
				DeviceID:           opts.deviceID,
				LaunchArgs:         opts.launchArgs,
				XCRunPath:          opts.xcrunPath,
				BenchmarkComponent: opts.benchmarkComponent,
			}
			metrics, err := ios.Run(ctx, cfg)
			if err != nil {
				return err
			}

			result := report.Result{
				Component:  component,
				IOS:        metrics,
				CLICommand: currentCLICommand(cmd),
			}
			fmt.Print(report.FormatSummary(result))
			if path, err := resolveOutputFile(component, "ios"); err != nil {
				return err
			} else if path != "" {
				if err := report.SaveJSON(path, result); err != nil {
					return err
				}
			}
			return nil
		},
	}
	opts.bind(cmd, "")
	return cmd
}

func newAllCmd() *cobra.Command {
	var aOpts androidOptions
	var iOpts iosOptions
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Run Android and iOS benchmarks sequentially.",
		RunE: func(cmd *cobra.Command, args []string) error {
			component := resolveComponent(defaultComponentName(aOpts.activity, iOpts.bundleID))
			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			result := report.Result{
				Component:  component,
				CLICommand: currentCLICommand(cmd),
			}

			if aOpts.packageName != "" || aOpts.activity != "" {
				cfg := android.Config{
					Component:          component,
					Package:            aOpts.packageName,
					Activity:           aOpts.activity,
					DeviceID:           aOpts.deviceID,
					ADBPath:            aOpts.adbPath,
					LaunchArgs:         aOpts.launchArgs,
					BenchmarkComponent: aOpts.benchmarkComponent,
				}
				metrics, err := android.Run(ctx, cfg)
				if err != nil {
					return err
				}
				result.Android = metrics
			}

			if iOpts.bundleID != "" {
				cfg := ios.Config{
					Component:          component,
					BundleID:           iOpts.bundleID,
					DeviceID:           iOpts.deviceID,
					LaunchArgs:         iOpts.launchArgs,
					XCRunPath:          iOpts.xcrunPath,
					BenchmarkComponent: iOpts.benchmarkComponent,
				}
				metrics, err := ios.Run(ctx, cfg)
				if err != nil {
					return err
				}
				result.IOS = metrics
			}

			if result.Android == nil && result.IOS == nil {
				return fmt.Errorf("provide Android and/or iOS configuration to run")
			}

			fmt.Print(report.FormatSummary(result))
			if path, err := resolveOutputFile(component, "all"); err != nil {
				return err
			} else if path != "" {
				if err := report.SaveJSON(path, result); err != nil {
					return err
				}
			}
			return nil
		},
	}

	aOpts.bind(cmd, "android-")
	iOpts.bind(cmd, "ios-")

	return cmd
}

func resolveComponent(fallback string) string {
	if componentFlag != "" {
		return componentFlag
	}
	if fallback != "" {
		return fallback
	}
	return "component"
}

func defaultComponentName(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func commandContext(cmd *cobra.Command) (context.Context, context.CancelFunc, error) {
	timeout := strings.TrimSpace(timeoutFlag)
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	if timeout == "" {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	dur, err := time.ParseDuration(timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid timeout %q: %w", timeout, err)
	}
	if dur <= 0 {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	ctx, cancel := context.WithTimeout(parent, dur)
	return ctx, cancel, nil
}

func resolveOutputFile(component string, platform string) (string, error) {
	path := strings.TrimSpace(outputPath)
	if path == "" {
		dir := strings.TrimSpace(reportsDir)
		if dir == "" {
			dir = defaultReportsDir
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create reports dir: %w", err)
		}
		filename := defaultReportFileName(component, platform)
		return filepath.Join(dir, filename), nil
	}

	if !filepath.IsAbs(path) {
		dir := strings.TrimSpace(reportsDir)
		if dir == "" {
			dir = defaultReportsDir
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create reports dir: %w", err)
		}
		path = filepath.Join(dir, path)
	} else {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create output dir: %w", err)
		}
	}
	return path, nil
}

func currentCLICommand(cmd *cobra.Command) string {
	if len(os.Args) == 0 {
		return ""
	}
	rootName := "designbench"
	if cmd != nil && cmd.Root() != nil && cmd.Root().Use != "" {
		rootName = cmd.Root().Use
	}
	var b strings.Builder
	b.WriteString(rootName)
	for _, arg := range os.Args[1:] {
		b.WriteByte(' ')
		b.WriteString(arg)
	}
	return b.String()
}

func defaultReportFileName(component string, platform string) string {
	componentToken := sanitizeToken(component, "component")
	platformToken := sanitizeToken(platform, "run")
	if platformToken != "" {
		return fmt.Sprintf("%s-%s.json", componentToken, platformToken)
	}
	return componentToken + ".json"
}

func sanitizeToken(value string, fallback string) string {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "" {
		return fallback
	}

	var b strings.Builder
	lastDash := false
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteByte(ch)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	token := strings.Trim(b.String(), "-")
	if token == "" {
		return fallback
	}
	return token
}

func newPreflightCmd() *cobra.Command {
	var (
		rootDir   string
		adbPath   string
		xcrunPath string
	)

	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Inspect project and connected devices to suggest ready-to-run commands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootDir == "" {
				rootDir = "."
			}
			absRoot, err := filepath.Abs(rootDir)
			if err != nil {
				return fmt.Errorf("resolve project root: %w", err)
			}

			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "Preflight (root: %s)\n\n", absRoot)

			androidProj, androidProjErr := preflight.DetectAndroidProject(absRoot)
			fmt.Fprintln(out, "Android:")
			if androidProj != nil {
				if androidProj.Package != "" {
					fmt.Fprintf(out, "  Package: %s\n", androidProj.Package)
				} else {
					fmt.Fprintln(out, "  Package: (not detected)")
				}
				activity := androidProj.Activity
				if activity == "" {
					activity = "<android-activity>"
				}
				fmt.Fprintf(out, "  Activity: %s\n", activity)
				if androidProj.ManifestPath != "" {
					fmt.Fprintf(out, "  Manifest: %s\n", androidProj.ManifestPath)
				}
				for _, warning := range androidProj.Warnings {
					fmt.Fprintf(out, "  ! %s\n", warning)
				}
			} else {
				fmt.Fprintln(out, "  Project: (not detected)")
			}
			if androidProjErr != nil {
				fmt.Fprintf(out, "  Note: %v\n", androidProjErr)
			}

			androidDevice, androidDeviceErr := preflight.DetectAndroidDevice(ctx, adbPath)
			if androidDeviceErr != nil {
				fmt.Fprintf(out, "  Device: %v\n", androidDeviceErr)
			} else {
				description := androidDevice.ID
				if androidDevice.Model != "" {
					description = fmt.Sprintf("%s (%s)", androidDevice.ID, androidDevice.Model)
				}
				fmt.Fprintf(out, "  Device: %s\n", description)
			}

			fmt.Fprintln(out, "\niOS:")
			iosProj, iosProjErr := preflight.DetectIOSProject(absRoot)
			if iosProjErr != nil {
				fmt.Fprintf(out, "  Project: %v\n", iosProjErr)
			} else {
				if iosProj.BundleID != "" {
					fmt.Fprintf(out, "  Bundle: %s\n", iosProj.BundleID)
				}
				if iosProj.InfoPlistPath != "" {
					fmt.Fprintf(out, "  Info.plist: %s\n", iosProj.InfoPlistPath)
				}
			}

			iosDevice, iosDeviceErr := preflight.DetectIOSDevice(ctx, xcrunPath)
			if iosDeviceErr != nil {
				fmt.Fprintf(out, "  Device: %v\n", iosDeviceErr)
			} else {
				description := iosDevice.UDID
				if iosDevice.Name != "" {
					description = fmt.Sprintf("%s (%s)", iosDevice.UDID, iosDevice.Name)
				}
				fmt.Fprintf(out, "  Device: %s\n", description)
				if iosDevice.Runtime != "" {
					fmt.Fprintf(out, "  Runtime: %s\n", iosDevice.Runtime)
				}
			}

			fmt.Fprintln(out)
			printPreflightSuggestions(out, androidProj, androidDevice, iosProj, iosDevice)
			return nil
		},
	}

	cmd.Flags().StringVar(&rootDir, "root", ".", "Project root to scan for configuration files.")
	cmd.Flags().StringVar(&adbPath, "android-adb", "adb", "Path to the adb binary for device detection.")
	cmd.Flags().StringVar(&xcrunPath, "ios-xcrun", "xcrun", "Path to the xcrun binary for simulator detection.")
	return cmd
}

func printPreflightSuggestions(out io.Writer,
	androidProj *preflight.AndroidProject,
	androidDevice *preflight.AndroidDevice,
	iosProj *preflight.IOSProject,
	iosDevice *preflight.IOSDevice,
) {
	androidCmd := buildAndroidSuggestion(androidProj, androidDevice)
	iosCmd := buildIOSSuggestion(iosProj, iosDevice)
	allCmd := buildAllSuggestion(androidProj, androidDevice, iosProj, iosDevice)

	if androidCmd == "" && iosCmd == "" && allCmd == "" {
		fmt.Fprintln(out, "No ready-to-run command suggestions available.")
		return
	}

	fmt.Fprintln(out, "Suggested commands:")
	first := true
	if androidCmd != "" {
		fmt.Fprintf(out, "  Android:\n    %s\n", androidCmd)
		first = false
	}
	if iosCmd != "" {
		if !first {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "  iOS:\n    %s\n", iosCmd)
		first = false
	}
	if allCmd != "" {
		if !first {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "  Cross-platform:\n    %s\n", allCmd)
	}
}

func buildAndroidSuggestion(proj *preflight.AndroidProject, device *preflight.AndroidDevice) string {
	if proj == nil && device == nil {
		return ""
	}
	pkg := "<android-package>"
	if proj != nil && proj.Package != "" {
		pkg = proj.Package
	}
	activity := "<android-activity>"
	if proj != nil && proj.Activity != "" {
		activity = proj.Activity
	}
	cmd := []string{"designbench", "android", "-p", pkg, "-a", activity}
	if device != nil && device.ID != "" {
		cmd = append(cmd, "--device", device.ID)
	}
	return strings.Join(cmd, " ")
}

func buildIOSSuggestion(proj *preflight.IOSProject, device *preflight.IOSDevice) string {
	if proj == nil && device == nil {
		return ""
	}
	bundle := "<ios-bundle>"
	if proj != nil && proj.BundleID != "" {
		bundle = proj.BundleID
	}
	cmd := []string{"designbench", "ios", "-b", bundle}
	if device != nil && device.UDID != "" {
		cmd = append(cmd, "--device", device.UDID)
	}
	return strings.Join(cmd, " ")
}

func buildAllSuggestion(androidProj *preflight.AndroidProject, androidDevice *preflight.AndroidDevice, iosProj *preflight.IOSProject, iosDevice *preflight.IOSDevice) string {
	if (androidProj == nil && androidDevice == nil) || (iosProj == nil && iosDevice == nil) {
		return ""
	}
	pkg := "<android-package>"
	if androidProj != nil && androidProj.Package != "" {
		pkg = androidProj.Package
	}
	activity := "<android-activity>"
	if androidProj != nil && androidProj.Activity != "" {
		activity = androidProj.Activity
	}
	bundle := "<ios-bundle>"
	if iosProj != nil && iosProj.BundleID != "" {
		bundle = iosProj.BundleID
	}
	cmd := []string{"designbench", "all", "--android-package", pkg, "--android-activity", activity, "--ios-bundle", bundle}
	if androidDevice != nil && androidDevice.ID != "" {
		cmd = append(cmd, "--android-device", androidDevice.ID)
	}
	if iosDevice != nil && iosDevice.UDID != "" {
		cmd = append(cmd, "--ios-device", iosDevice.UDID)
	}
	return strings.Join(cmd, " ")
}
