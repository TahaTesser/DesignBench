package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	viewFlag      string
	outputPath    string
	timeoutFlag   string
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
	cmd.PersistentFlags().StringVar(&viewFlag, "view", "", "UI view identifier forwarded to benchmark harnesses on each platform.")
	cmd.PersistentFlags().StringVarP(&outputPath, "output", "o", "", "Write JSON report to this exact path (defaults to ./designbench-reports/<component>-<platform>.json).")
	cmd.PersistentFlags().StringVar(&timeoutFlag, "timeout", "60s", "Overall command timeout (e.g. 45s, 2m).")

	cmd.AddCommand(newAndroidCmd(), newIOSCmd(), newPreflightCmd())

	return cmd
}

type androidOptions struct {
	packageName     string
	activity        string
	deviceID        string
	adbPath         string
	install         bool
	gradlePath      string
	installTask     string
	projectRoot     string
	detectedProject *preflight.AndroidProject
}

type iosOptions struct {
	bundleID  string
	deviceID  string
	xcrunPath string
}

func newAndroidCmd() *cobra.Command {
	var opts androidOptions
	opts.adbPath = "adb"
	opts.gradlePath = "./gradlew"
	cmd := &cobra.Command{
		Use:   "android",
		Short: "Run Android render benchmark.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureAndroidDefaults(&opts); err != nil {
				return err
			}
			component := resolveComponent(opts.activity)
			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			benchmarkComponent := viewFlag

			if opts.install {
				if err := runAndroidInstall(ctx, cmd, &opts); err != nil {
					return err
				}
			}

			cfg := android.Config{
				Component:          component,
				Package:            opts.packageName,
				Activity:           opts.activity,
				DeviceID:           opts.deviceID,
				ADBPath:            opts.adbPath,
				LaunchArgs:         nil,
				BenchmarkComponent: benchmarkComponent,
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
	cmd.Flags().BoolVar(&opts.install, "install", false, "Run the detected Gradle installRelease task before benchmarking.")
	return cmd
}

func newIOSCmd() *cobra.Command {
	var opts iosOptions
	opts.xcrunPath = "xcrun"
	cmd := &cobra.Command{
		Use:   "ios",
		Short: "Run iOS render benchmark.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureIOSDefaults(&opts); err != nil {
				return err
			}
			component := resolveComponent(opts.bundleID)
			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			benchmarkComponent := viewFlag

			cfg := ios.Config{
				Component:          component,
				BundleID:           opts.bundleID,
				DeviceID:           opts.deviceID,
				LaunchArgs:         nil,
				XCRunPath:          opts.xcrunPath,
				BenchmarkComponent: benchmarkComponent,
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
	return cmd
}

func ensureAndroidDefaults(opts *androidOptions) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}
	absRoot, err := filepath.Abs(root)
	if err == nil {
		root = absRoot
	}
	opts.projectRoot = root
	proj, detectErr := preflight.DetectAndroidProject(root)
	if detectErr == nil {
		opts.detectedProject = proj
	}
	missingPackage := strings.TrimSpace(opts.packageName) == ""
	missingActivity := strings.TrimSpace(opts.activity) == ""
	if !missingPackage && !missingActivity {
		return nil
	}
	if detectErr == nil && proj != nil {
		if missingPackage && proj.Package != "" {
			opts.packageName = proj.Package
		}
		if missingActivity && proj.Activity != "" {
			opts.activity = proj.Activity
		}
	}
	if strings.TrimSpace(opts.packageName) != "" && strings.TrimSpace(opts.activity) != "" {
		return nil
	}
	if detectErr != nil {
		return fmt.Errorf("unable to auto-detect Android defaults: %w (set --package/--activity manually)", detectErr)
	}
	missing := make([]string, 0, 2)
	if strings.TrimSpace(opts.packageName) == "" {
		missing = append(missing, "--package")
	}
	if strings.TrimSpace(opts.activity) == "" {
		missing = append(missing, "--activity")
	}
	return fmt.Errorf("missing Android %s (run from project root or provide flags)", strings.Join(missing, " and "))
}

func ensureIOSDefaults(opts *iosOptions) error {
	if strings.TrimSpace(opts.bundleID) != "" {
		return nil
	}
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}
	proj, detectErr := preflight.DetectIOSProject(root)
	if detectErr != nil {
		return fmt.Errorf("unable to auto-detect iOS bundle id: %w (set --bundle manually)", detectErr)
	}
	if proj == nil || strings.TrimSpace(proj.BundleID) == "" {
		return fmt.Errorf("Info.plist detected but bundle id empty (set --bundle manually)")
	}
	opts.bundleID = proj.BundleID
	return nil
}

func resolveComponent(fallback string) string {
	if componentFlag != "" {
		return componentFlag
	}
	if viewFlag != "" {
		return viewFlag
	}
	if fallback != "" {
		return fallback
	}
	return "component"
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
		if err := os.MkdirAll(defaultReportsDir, 0o755); err != nil {
			return "", fmt.Errorf("create reports dir: %w", err)
		}
		filename := defaultReportFileName(component, platform)
		return filepath.Join(defaultReportsDir, filename), nil
	}

	if !filepath.IsAbs(path) {
		if err := os.MkdirAll(defaultReportsDir, 0o755); err != nil {
			return "", fmt.Errorf("create reports dir: %w", err)
		}
		path = filepath.Join(defaultReportsDir, path)
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
	rootDir := "."
	adbPath := "adb"
	xcrunPath := "xcrun"

	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Run a readiness checklist for Android and iOS benchmarking.",
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

			androidProj, androidProjErr := preflight.DetectAndroidProject(absRoot)
			androidDevice, androidDeviceErr := preflight.DetectAndroidDevice(ctx, adbPath)
			iosProj, iosProjErr := preflight.DetectIOSProject(absRoot)
			iosDevice, iosDeviceErr := preflight.DetectIOSDevice(ctx, xcrunPath)

			items := []checklistItem{
				checkBinaryItem("adb available", adbPath),
				checkBinaryItem("xcodebuild available", "xcodebuild"),
				checkBinaryItem("xcrun available", xcrunPath),
				checkAndroidProjectItem(androidProj, androidProjErr),
				checkAndroidDeviceItem(androidDevice, androidDeviceErr),
				checkIOSProjectItem(iosProj, iosProjErr),
				checkIOSDeviceItem(iosDevice, iosDeviceErr),
			}

			fmt.Fprintf(out, "Preflight checklist (root: %s)\n\n", absRoot)
			printChecklist(out, items)
			return nil
		},
	}

	return cmd
}

func runAndroidInstall(ctx context.Context, cobraCmd *cobra.Command, opts *androidOptions) error {
	gradle := strings.TrimSpace(opts.gradlePath)
	if gradle == "" {
		gradle = "./gradlew"
	}
	task := strings.TrimSpace(opts.installTask)
	if task == "" {
		task = defaultAndroidInstallTask(opts.detectedProject)
	}
	if task == "" {
		return fmt.Errorf("unable to determine Gradle install task; provide --install-task")
	}
	root := opts.projectRoot
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	args := strings.Fields(task)
	if len(args) == 0 {
		return fmt.Errorf("install task cannot be empty")
	}
	cobraCmd.Printf("Running %s %s to install Android release benchmark build...\n", gradle, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, gradle, args...)
	cmd.Dir = root
	cmd.Stdout = cobraCmd.OutOrStdout()
	cmd.Stderr = cobraCmd.ErrOrStderr()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gradle install failed: %w", err)
	}
	return nil
}

func defaultAndroidInstallTask(proj *preflight.AndroidProject) string {
	if proj == nil {
		return ""
	}
	module := strings.TrimSpace(proj.ModuleDir)
	if module == "" {
		return "installRelease"
	}
	module = filepath.ToSlash(module)
	module = strings.Trim(module, "/")
	if module == "" {
		return "installRelease"
	}
	module = strings.ReplaceAll(module, "/", ":")
	return fmt.Sprintf(":%s:installRelease", module)
}

type checklistStatus int

const (
	statusPass checklistStatus = iota
	statusWarn
	statusFail
)

func (s checklistStatus) label() string {
	switch s {
	case statusPass:
		return "PASS"
	case statusWarn:
		return "WARN"
	case statusFail:
		return "FAIL"
	default:
		return "????"
	}
}

type checklistItem struct {
	name   string
	status checklistStatus
	notes  []string
}

func newChecklistItem(name string, status checklistStatus, notes ...string) checklistItem {
	return checklistItem{name: name, status: status, notes: notes}
}

func printChecklist(out io.Writer, items []checklistItem) {
	for _, item := range items {
		fmt.Fprintf(out, "[%s] %s\n", item.status.label(), item.name)
		for _, note := range item.notes {
			fmt.Fprintf(out, "    - %s\n", note)
		}
	}
}

func checkBinaryItem(label, path string) checklistItem {
	if strings.TrimSpace(path) == "" {
		return newChecklistItem(label, statusFail, "no path configured")
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return newChecklistItem(label, statusFail, err.Error())
	}
	return newChecklistItem(label, statusPass, fmt.Sprintf("path: %s", resolved))
}

func checkAndroidProjectItem(proj *preflight.AndroidProject, err error) checklistItem {
	if err != nil {
		return newChecklistItem("Android project", statusFail, err.Error())
	}
	if proj == nil {
		return newChecklistItem("Android project", statusWarn, "AndroidManifest.xml not found (run from project root?)")
	}
	notes := make([]string, 0, 3)
	if proj.Package != "" {
		notes = append(notes, fmt.Sprintf("Package: %s", proj.Package))
	}
	if proj.Activity != "" {
		notes = append(notes, fmt.Sprintf("Activity: %s", proj.Activity))
	}
	if proj.ModuleDir != "" {
		notes = append(notes, fmt.Sprintf("Gradle module: %s", proj.ModuleDir))
	}
	return newChecklistItem("Android project", statusPass, notes...)
}

func checkAndroidDeviceItem(device *preflight.AndroidDevice, err error) checklistItem {
	if err != nil {
		return newChecklistItem("Android device detected", statusFail, err.Error())
	}
	if device == nil {
		return newChecklistItem("Android device detected", statusWarn, "No devices reported by `adb devices -l`.")
	}
	desc := device.ID
	if device.Model != "" {
		desc = fmt.Sprintf("%s (%s)", device.ID, device.Model)
	}
	return newChecklistItem("Android device detected", statusPass, desc)
}

func checkIOSProjectItem(proj *preflight.IOSProject, err error) checklistItem {
	if err != nil {
		return newChecklistItem("iOS project", statusFail, err.Error())
	}
	if proj == nil {
		return newChecklistItem("iOS project", statusWarn, "Info.plist not found")
	}
	notes := make([]string, 0, 2)
	if proj.BundleID != "" {
		notes = append(notes, fmt.Sprintf("Bundle ID: %s", proj.BundleID))
	}
	if proj.InfoPlistPath != "" {
		notes = append(notes, fmt.Sprintf("Info.plist: %s", proj.InfoPlistPath))
	}
	return newChecklistItem("iOS project", statusPass, notes...)
}

func checkIOSDeviceItem(device *preflight.IOSDevice, err error) checklistItem {
	if err != nil {
		return newChecklistItem("iOS device detected", statusFail, err.Error())
	}
	if device == nil {
		return newChecklistItem("iOS device detected", statusWarn, "No booted simulator or connected device reported by xcrun.")
	}
	desc := device.UDID
	if device.Name != "" {
		desc = fmt.Sprintf("%s (%s)", device.UDID, device.Name)
	}
	if device.Runtime != "" {
		desc = fmt.Sprintf("%s – %s", desc, device.Runtime)
	}
	return newChecklistItem("iOS device detected", statusPass, desc)
}
