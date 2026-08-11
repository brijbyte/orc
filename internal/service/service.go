package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/brijbyte/orc/internal/config"
)

const (
	label    = "dev.orc.web"
	unitName = "orc.service"
)

type Options struct {
	Executable string
	Addr       string
	Domain     string
	Cwd        string
}

type Status struct {
	Installed bool
	Running   bool
	Detail    string
}

func URLPath() string { return config.Path("service.url") }
func LogPath() string { return config.Path("service.log") }

func Install(opts Options) error {
	if err := validate(opts); err != nil {
		return err
	}
	if err := os.MkdirAll(config.Home(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(config.Home(), 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	log.Close()
	if err := os.Chmod(LogPath(), 0o600); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return installDarwin(opts)
	case "linux":
		return installLinux(opts)
	default:
		return fmt.Errorf("services are not supported on %s", runtime.GOOS)
	}
}

func Start() error {
	os.Remove(URLPath())
	switch runtime.GOOS {
	case "darwin":
		return startDarwin()
	case "linux":
		return run("systemctl", "--user", "start", unitName)
	default:
		return fmt.Errorf("services are not supported on %s", runtime.GOOS)
	}
}

func Stop() error {
	switch runtime.GOOS {
	case "darwin":
		if err := run("launchctl", "bootout", launchTarget()); err != nil && isRunningDarwin() {
			return err
		}
		return nil
	case "linux":
		return run("systemctl", "--user", "stop", unitName)
	default:
		return fmt.Errorf("services are not supported on %s", runtime.GOOS)
	}
}

func Restart() error {
	os.Remove(URLPath())
	switch runtime.GOOS {
	case "darwin":
		if isRunningDarwin() {
			return run("launchctl", "kickstart", "-k", launchTarget())
		}
		return startDarwin()
	case "linux":
		return run("systemctl", "--user", "restart", unitName)
	default:
		return fmt.Errorf("services are not supported on %s", runtime.GOOS)
	}
}

func Uninstall() error {
	switch runtime.GOOS {
	case "darwin":
		if isRunningDarwin() {
			if err := Stop(); err != nil {
				return err
			}
		}
		if err := os.Remove(darwinPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	case "linux":
		if fileExists(linuxPath()) {
			if err := run("systemctl", "--user", "disable", "--now", unitName); err != nil {
				return err
			}
		}
		if err := os.Remove(linuxPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("services are not supported on %s", runtime.GOOS)
	}
	os.Remove(URLPath())
	return nil
}

func GetStatus() Status {
	switch runtime.GOOS {
	case "darwin":
		out, err := command("launchctl", "print", launchTarget())
		return Status{Installed: fileExists(darwinPath()), Running: err == nil && strings.Contains(out, "state = running")}
	case "linux":
		out, err := command("systemctl", "--user", "is-active", unitName)
		return Status{Installed: fileExists(linuxPath()), Running: err == nil && strings.TrimSpace(out) == "active"}
	default:
		return Status{Detail: "unsupported on " + runtime.GOOS}
	}
}

func validate(opts Options) error {
	for name, value := range map[string]string{"executable": opts.Executable, "address": opts.Addr, "working directory": opts.Cwd} {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	st, err := os.Stat(opts.Cwd)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("bad working directory %q", opts.Cwd)
	}
	return nil
}

func args(opts Options) []string {
	a := []string{opts.Executable, "--serve=" + opts.Addr, "--service-file=" + URLPath()}
	if opts.Domain != "" {
		a = append(a, "--domain="+opts.Domain)
	}
	return a
}

func installLinux(opts Options) error {
	path := linuxPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var execArgs []string
	for _, arg := range args(opts) {
		execArgs = append(execArgs, systemdQuote(arg))
	}
	var environment strings.Builder
	for _, item := range serviceEnv() {
		fmt.Fprintf(&environment, "Environment=%s\n", systemdQuote(item[0]+"="+item[1]))
	}
	unit := `[Unit]
Description=orc web service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + strings.Join(execArgs, " ") + `
WorkingDirectory=` + systemdQuote(opts.Cwd) + `
` + environment.String() + `StandardOutput=` + systemdQuote("append:"+LogPath()) + `
StandardError=` + systemdQuote("append:"+LogPath()) + `
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
`
	if err := writeFile(path, []byte(unit)); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "enable", unitName); err != nil {
		return err
	}
	return run("systemctl", "--user", "restart", unitName)
}

func installDarwin(opts Options) error {
	path := darwinPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var argv, environment strings.Builder
	for _, arg := range args(opts) {
		fmt.Fprintf(&argv, "\n      <string>%s</string>", xmlEscape(arg))
	}
	for _, item := range serviceEnv() {
		fmt.Fprintf(&environment, "\n    <key>%s</key><string>%s</string>", xmlEscape(item[0]), xmlEscape(item[1]))
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + label + `</string>
  <key>ProgramArguments</key>
  <array>` + argv.String() + `
  </array>
  <key>WorkingDirectory</key><string>` + xmlEscape(opts.Cwd) + `</string>
  <key>EnvironmentVariables</key>
  <dict>` + environment.String() + `
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>` + xmlEscape(LogPath()) + `</string>
  <key>StandardErrorPath</key><string>` + xmlEscape(LogPath()) + `</string>
</dict>
</plist>
`
	if isRunningDarwin() {
		_ = run("launchctl", "bootout", launchTarget())
	}
	if err := writeFile(path, []byte(plist)); err != nil {
		return err
	}
	return startDarwin()
}

func startDarwin() error {
	if !fileExists(darwinPath()) {
		return errors.New("service is not installed")
	}
	if isRunningDarwin() {
		return run("launchctl", "kickstart", "-k", launchTarget())
	}
	return run("launchctl", "bootstrap", launchDomain(), darwinPath())
}

func isRunningDarwin() bool {
	_, err := command("launchctl", "print", launchTarget())
	return err == nil
}

func launchDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
func launchTarget() string { return launchDomain() + "/" + label }

func darwinPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func linuxPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(base) {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", unitName)
}

func serviceEnv() [][2]string {
	var env [][2]string
	for _, key := range []string{"PATH", "SHELL", "XDG_CONFIG_HOME", "ORC_PROVIDER", "ORC_MODEL"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, [2]string{key, value})
		}
	}
	return env
}

func systemdQuote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(s)
	return `"` + s + `"`
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func run(name string, args ...string) error {
	out, err := command(name, args...)
	if err != nil {
		if out = strings.TrimSpace(out); out != "" {
			return fmt.Errorf("%s: %s", name, out)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func command(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
