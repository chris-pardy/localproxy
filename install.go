package main

import (
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
		fmt.Fprintln(os.Stderr, "install requires root. Run: sudo localproxy install")
		os.Exit(1)
	}

	// Find our own binary
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot find executable: %v\n", err)
		os.Exit(1)
	}
	self, _ = filepath.EvalSymlinks(self)

	// Detect roots from SUDO_USER's home
	roots := "/Users"
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		roots = fmt.Sprintf("/Users/%s/Code", sudoUser)
	}

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
