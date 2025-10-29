package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tahatesser/designbench/pkg/android"
	"github.com/tahatesser/designbench/pkg/ios"
	"github.com/tahatesser/designbench/pkg/report"
)

var (
	componentFlag string
	outputPath    string
	timeoutFlag   string
	reportsDir    string
)

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
	cmd.PersistentFlags().StringVarP(&outputPath, "output", "o", "", "Optional JSON output file name (stored under reports-dir unless absolute).")
	cmd.PersistentFlags().StringVar(&timeoutFlag, "timeout", "60s", "Overall command timeout (e.g. 45s, 2m).")
	cmd.PersistentFlags().StringVar(&reportsDir, "reports-dir", "reports", "Directory where JSON reports are written.")

	cmd.AddCommand(newAndroidCmd(), newIOSCmd(), newAllCmd())

	return cmd
}

type androidOptions struct {
	packageName string
	activity    string
	deviceID    string
	adbPath     string
	launchArgs  []string
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
}

type iosOptions struct {
	bundleID   string
	deviceID   string
	xcrunPath  string
	launchArgs []string
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
				Component:  component,
				Package:    opts.packageName,
				Activity:   opts.activity,
				DeviceID:   opts.deviceID,
				ADBPath:    opts.adbPath,
				LaunchArgs: opts.launchArgs,
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
				Component:  component,
				BundleID:   opts.bundleID,
				DeviceID:   opts.deviceID,
				LaunchArgs: opts.launchArgs,
				XCRunPath:  opts.xcrunPath,
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
					Component:  component,
					Package:    aOpts.packageName,
					Activity:   aOpts.activity,
					DeviceID:   aOpts.deviceID,
					ADBPath:    aOpts.adbPath,
					LaunchArgs: aOpts.launchArgs,
				}
				metrics, err := android.Run(ctx, cfg)
				if err != nil {
					return err
				}
				result.Android = metrics
			}

			if iOpts.bundleID != "" {
				cfg := ios.Config{
					Component:  component,
					BundleID:   iOpts.bundleID,
					DeviceID:   iOpts.deviceID,
					LaunchArgs: iOpts.launchArgs,
					XCRunPath:  iOpts.xcrunPath,
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

func resolveOutputFile(_ string, _ string) (string, error) {
	if outputPath == "" {
		return "", nil
	}
	path := outputPath
	if !filepath.IsAbs(path) {
		if reportsDir == "" {
			reportsDir = "reports"
		}
		if err := os.MkdirAll(reportsDir, 0o755); err != nil {
			return "", fmt.Errorf("create reports dir: %w", err)
		}
		path = filepath.Join(reportsDir, path)
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
