package preflight

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	bundleKeyRe           = regexp.MustCompile(`<key>CFBundleIdentifier</key>\s*<string>(.*?)</string>`)
	namespaceAssignRe     = regexp.MustCompile(`namespace\s*=\s*"([^"]+)"`)
	namespaceCallRe       = regexp.MustCompile(`namespace\s+"([^"]+)"`)
	applicationIDAssignRe = regexp.MustCompile(`applicationId\s*=\s*"([^"]+)"`)
	applicationIDCallRe   = regexp.MustCompile(`applicationId\s+"([^"]+)"`)
)

const androidNamespace = "http://schemas.android.com/apk/res/android"

var errManifestPackageMissing = errors.New("android manifest package attribute not found")

// AndroidProject captures basic metadata extracted from AndroidManifest.xml.
type AndroidProject struct {
	Package      string
	Activity     string
	ManifestPath string
	ModuleDir    string
	Warnings     []string
}

type manifestActivity struct {
	name        string
	hasMain     bool
	hasLauncher bool
}

// AndroidDevice describes a connected Android device.
type AndroidDevice struct {
	ID          string
	Model       string
	Product     string
	Description string
}

// IOSProject captures bundle identifier details from Info.plist.
type IOSProject struct {
	BundleID      string
	InfoPlistPath string
}

// IOSDevice describes a booted simulator or connected device.
type IOSDevice struct {
	UDID    string
	Name    string
	State   string
	Runtime string
}

// DetectAndroidProject attempts to locate an AndroidManifest.xml and extract the package and main activity.
func DetectAndroidProject(root string) (*AndroidProject, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve android root: %w", err)
	}
	root = absRoot
	paths := []string{
		"androidApp/src/main/AndroidManifest.xml",
		"androidApp/src/androidMain/AndroidManifest.xml",
		"app/src/main/AndroidManifest.xml",
		"AndroidManifest.xml",
	}
	for _, rel := range paths {
		path := filepath.Join(root, rel)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return loadAndroidProject(path, root)
		}
	}

	var manifestPath string
	stopErr := errors.New("designbench:found-manifest")
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip typical build output directories.
			name := d.Name()
			switch name {
			case ".git", "build", "gradle", ".gradle", ".idea", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), "AndroidManifest.xml") {
			manifestPath = path
			return stopErr
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, stopErr) && walkErr != filepath.SkipDir {
		return nil, walkErr
	}
	if manifestPath != "" {
		return loadAndroidProject(manifestPath, root)
	}
	return nil, fmt.Errorf("android manifest not found")
}

func loadAndroidProject(path, root string) (*AndroidProject, error) {
	project, err := parseAndroidManifest(path)
	if err != nil && !errors.Is(err, errManifestPackageMissing) {
		return nil, err
	}
	if project == nil {
		project = &AndroidProject{ManifestPath: path}
	}
	project.ModuleDir = resolveGradleModuleDir(path, root)
	if project.Package == "" {
		if pkg := derivePackageFromGradle(path); pkg != "" {
			project.Package = pkg
			project.Warnings = append(project.Warnings, fmt.Sprintf("manifest missing package; using Gradle namespace %q", pkg))
			return project, nil
		}
		if err == nil {
			project.Warnings = append(project.Warnings, fmt.Sprintf("package attribute missing in %s", path))
			err = errManifestPackageMissing
		}
	}
	return project, err
}

func parseAndroidManifest(path string) (*AndroidProject, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	defer f.Close()

	project := &AndroidProject{ManifestPath: path}
	decoder := xml.NewDecoder(f)

	var currentActivity *manifestActivity
	activities := make([]manifestActivity, 0)
	var intentFilterDepth int

	for {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			if errors.Is(tokenErr, os.ErrClosed) {
				break
			}
			if errors.Is(tokenErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse manifest xml: %w", tokenErr)
		}

		switch tok := token.(type) {
		case xml.StartElement:
			switch tok.Name.Local {
			case "manifest":
				for _, attr := range tok.Attr {
					if attr.Name.Local == "package" && strings.TrimSpace(attr.Value) != "" {
						project.Package = strings.TrimSpace(attr.Value)
						break
					}
				}
			case "activity":
				info := manifestActivity{}
				for _, attr := range tok.Attr {
					if attr.Name.Local == "name" {
						info.name = strings.TrimSpace(attr.Value)
					} else if attr.Name.Space == androidNamespace && attr.Name.Local == "name" {
						info.name = strings.TrimSpace(attr.Value)
					}
				}
				currentActivity = &info
			case "intent-filter":
				if currentActivity != nil {
					intentFilterDepth++
				}
			case "action":
				if currentActivity != nil && intentFilterDepth > 0 {
					for _, attr := range tok.Attr {
						if (attr.Name.Space == androidNamespace || attr.Name.Space == "") && attr.Name.Local == "name" {
							if attr.Value == "android.intent.action.MAIN" {
								currentActivity.hasMain = true
							}
						}
					}
				}
			case "category":
				if currentActivity != nil && intentFilterDepth > 0 {
					for _, attr := range tok.Attr {
						if (attr.Name.Space == androidNamespace || attr.Name.Space == "") && attr.Name.Local == "name" {
							if attr.Value == "android.intent.category.LAUNCHER" {
								currentActivity.hasLauncher = true
							}
						}
					}
				}
			}
		case xml.EndElement:
			switch tok.Name.Local {
			case "intent-filter":
				if intentFilterDepth > 0 {
					intentFilterDepth--
				}
			case "activity":
				if currentActivity != nil {
					activities = append(activities, *currentActivity)
					currentActivity = nil
				}
			}
		}
	}

	project.Activity = selectPrimaryActivity(activities)

	if project.Package == "" {
		project.Warnings = append(project.Warnings, fmt.Sprintf("package attribute not found in %s", path))
		return project, errManifestPackageMissing
	}
	return project, nil
}

func selectPrimaryActivity(activities []manifestActivity) string {
	if len(activities) == 0 {
		return ""
	}
	for _, act := range activities {
		if act.hasMain && act.hasLauncher {
			return act.name
		}
	}
	return activities[0].name
}

func derivePackageFromGradle(manifestPath string) string {
	dir := filepath.Dir(manifestPath)
	visited := 0
	for {
		if dir == "" || dir == "." {
			break
		}
		for _, name := range []string{"build.gradle.kts", "build.gradle"} {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if pkg := parseNamespaceFromGradle(string(data)); pkg != "" {
				return pkg
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir || visited > 5 {
			break
		}
		dir = parent
		visited++
	}
	return ""
}

func resolveGradleModuleDir(manifestPath, root string) string {
	root = filepath.Clean(root)
	infoRoot := root
	if infoRoot == "" {
		if abs, err := filepath.Abs("."); err == nil {
			infoRoot = abs
		}
	}
	dir := filepath.Dir(manifestPath)
	for {
		if hasGradleBuildFile(dir) {
			rel, err := filepath.Rel(infoRoot, dir)
			if err == nil {
				if rel == "." {
					return ""
				}
				return rel
			}
			return dir
		}
		if dir == infoRoot {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if hasGradleBuildFile(infoRoot) {
		return ""
	}
	return ""
}

func hasGradleBuildFile(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{"build.gradle.kts", "build.gradle"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func parseNamespaceFromGradle(content string) string {
	if match := namespaceAssignRe.FindStringSubmatch(content); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if match := namespaceCallRe.FindStringSubmatch(content); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if match := applicationIDAssignRe.FindStringSubmatch(content); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if match := applicationIDCallRe.FindStringSubmatch(content); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

// DetectAndroidDevice returns the first connected Android device reported by `adb devices -l`.
func DetectAndroidDevice(ctx context.Context, adbPath string) (*AndroidDevice, error) {
	cmd := exec.CommandContext(ctx, adbPath, "devices", "-l")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("detect android devices: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines[1:] { // skip header
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		status := fields[1]
		if status != "device" {
			continue
		}
		device := &AndroidDevice{
			ID:          fields[0],
			Description: line,
		}
		for _, field := range fields[2:] {
			if strings.HasPrefix(field, "model:") {
				device.Model = strings.TrimPrefix(field, "model:")
			}
			if strings.HasPrefix(field, "product:") {
				device.Product = strings.TrimPrefix(field, "product:")
			}
		}
		return device, nil
	}
	return nil, fmt.Errorf("no Android devices found (ensure adb device is connected)")
}

// DetectIOSProject attempts to locate an Info.plist and extract the CFBundleIdentifier.
func DetectIOSProject(root string) (*IOSProject, error) {
	paths := []string{
		"iosApp/iosApp/Info.plist",
		"iosApp/Info.plist",
		"ios/Info.plist",
	}
	for _, rel := range paths {
		path := filepath.Join(root, rel)
		if _, err := os.Stat(path); err == nil {
			proj, err := parseInfoPlist(path)
			if err != nil {
				return nil, err
			}
			return proj, nil
		}
	}

	var foundPath string
	stopErr := errors.New("designbench:found-infoplist")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", "DerivedData", ".idea", "Pods", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "Info.plist" {
			foundPath = path
			return stopErr
		}
		return nil
	})
	if err != nil && !errors.Is(err, stopErr) && err != filepath.SkipDir {
		return nil, err
	}
	if foundPath == "" {
		return nil, fmt.Errorf("info.plist not found")
	}
	return parseInfoPlist(foundPath)
}

func parseInfoPlist(path string) (*IOSProject, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read info.plist: %w", err)
	}
	content := string(data)
	match := bundleKeyRe.FindStringSubmatch(content)
	if len(match) < 2 {
		return nil, fmt.Errorf("CFBundleIdentifier not found in %s", path)
	}
	bundleID := strings.TrimSpace(match[1])
	if bundleID == "" {
		return nil, fmt.Errorf("empty CFBundleIdentifier in %s", path)
	}
	return &IOSProject{
		BundleID:      bundleID,
		InfoPlistPath: path,
	}, nil
}

type simctlDevice struct {
	UDID    string `json:"udid"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Runtime string `json:"runtime"`
}

type simctlList struct {
	Devices map[string][]simctlDevice `json:"devices"`
}

// DetectIOSDevice finds the first booted simulator using `xcrun simctl list devices --json`.
func DetectIOSDevice(ctx context.Context, xcrunPath string) (*IOSDevice, error) {
	cmd := exec.CommandContext(ctx, xcrunPath, "simctl", "list", "devices", "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list simulators: %w", err)
	}
	var payload simctlList
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, fmt.Errorf("parse simctl output: %w", err)
	}
	for _, devices := range payload.Devices {
		for _, device := range devices {
			if strings.EqualFold(device.State, "Booted") {
				return &IOSDevice{
					UDID:    device.UDID,
					Name:    device.Name,
					State:   device.State,
					Runtime: device.Runtime,
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("no booted iOS simulators found (launch one via Simulator.app or specify --ios-device)")
}
