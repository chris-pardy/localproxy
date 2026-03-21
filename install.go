package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	binaryDest  = "/usr/local/bin/localproxy"
	plistPath   = "/Library/LaunchDaemons/com.localproxy.daemon.plist"
	logPath     = "/var/log/localproxy.log"
	serviceLabel = "com.localproxy.daemon"
)

func runInstall() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "install requires root. Run: sudo localproxy install -roots ~/Code")
		os.Exit(1)
	}

	// Parse install-specific flags
	installFlags := flag.NewFlagSet("install", flag.ExitOnError)
	rootsFlag := installFlags.String("roots", "", "comma-separated root directories to scan (e.g. ~/Code,~/Projects)")
	installFlags.Parse(os.Args[2:])

	// Find our own binary
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot find executable: %v\n", err)
		os.Exit(1)
	}
	self, _ = filepath.EvalSymlinks(self)

	// Resolve roots: flag > existing plist > SUDO_USER default
	roots := *rootsFlag
	if roots == "" {
		roots = readExistingRoots()
	}
	if roots == "" {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			roots = fmt.Sprintf("/Users/%s/Code", sudoUser)
		}
	}
	if roots == "" {
		fmt.Fprintln(os.Stderr, "cannot detect root directories. Use: sudo localproxy install -roots ~/Code")
		os.Exit(1)
	}

	// Expand ~ in each root path
	roots = expandRoots(roots)

	// Check if already installed
	updating := false
	if _, err := os.Stat(plistPath); err == nil {
		updating = true
		// Unload first
		exec.Command("launchctl", "bootout", "system/"+serviceLabel).Run()
	}

	// Copy binary then sync to ensure the write is fully flushed before signing
	if err := copyFile(self, binaryDest); err != nil {
		fmt.Fprintf(os.Stderr, "failed to copy binary: %v\n", err)
		os.Exit(1)
	}
	os.Chmod(binaryDest, 0755)
	exec.Command("sync").Run()

	// Ad-hoc codesign so macOS allows LaunchDaemon execution
	if out, err := exec.Command("codesign", "-s", "-", "-f", binaryDest).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "codesign failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	// Generate plist
	plist := generatePlist(roots)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write plist: %v\n", err)
		os.Exit(1)
	}

	// Load
	if out, err := exec.Command("launchctl", "bootstrap", "system", plistPath).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "launchctl bootstrap failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	if updating {
		fmt.Println("localproxy updated and restarted")
	} else {
		fmt.Println("localproxy installed and started")
	}
	fmt.Printf("  binary:  %s\n", binaryDest)
	fmt.Printf("  plist:   %s\n", plistPath)
	fmt.Printf("  logs:    %s\n", logPath)
	fmt.Printf("  roots:   %s\n", roots)
	fmt.Println("\nVisit http://localhost to see the dashboard")
}

func runUninstall() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "uninstall requires root. Run: sudo localproxy uninstall")
		os.Exit(1)
	}

	exec.Command("launchctl", "bootout", "system/"+serviceLabel).Run()
	os.Remove(plistPath)
	os.Remove(binaryDest)

	fmt.Println("localproxy uninstalled")
}

func generatePlist(roots string) string {
	tmpl := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{LABEL}}</string>
    <key>Program</key>
    <string>{{BINARY}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{BINARY}}</string>
        <string>-roots</string>
        <string>{{ROOTS}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{LOG}}</string>
    <key>StandardErrorPath</key>
    <string>{{LOG}}</string>
    <key>SoftResourceLimits</key>
    <dict>
        <key>NumberOfFiles</key>
        <integer>8192</integer>
    </dict>
</dict>
</plist>`

	r := strings.NewReplacer(
		"{{LABEL}}", serviceLabel,
		"{{BINARY}}", binaryDest,
		"{{ROOTS}}", roots,
		"{{LOG}}", logPath,
	)
	return r.Replace(tmpl)
}

// readExistingRoots extracts the -roots value from an existing plist so
// that reinstalls/updates preserve the user's configuration.
func readExistingRoots() string {
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return ""
	}
	// Find the -roots argument in the plist XML
	content := string(data)
	marker := "<string>-roots</string>"
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(marker):]
	// Next <string>...</string> is the value
	start := strings.Index(rest, "<string>")
	end := strings.Index(rest, "</string>")
	if start < 0 || end < 0 || end <= start+8 {
		return ""
	}
	return rest[start+8 : end]
}

// expandRoots expands ~ to the invoking user's home directory in each root path.
func expandRoots(roots string) string {
	home := ""
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		home = "/Users/" + sudoUser
	} else if h, err := os.UserHomeDir(); err == nil {
		home = h
	}
	if home == "" {
		return roots
	}

	var expanded []string
	for _, r := range strings.Split(roots, ",") {
		r = strings.TrimSpace(r)
		if strings.HasPrefix(r, "~/") {
			r = home + r[1:]
		}
		expanded = append(expanded, r)
	}
	return strings.Join(expanded, ",")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
