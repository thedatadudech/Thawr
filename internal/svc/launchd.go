package svc

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Where launchd daemons and their logs live.
const (
	launchDaemonsDir = "/Library/LaunchDaemons"
	launchdLogDir    = "/Library/Logs/Thawr"
)

// launchd drives launchctl and writes plists under Root.
type launchd struct{ opts Options }

func newLaunchd(opts Options) *launchd { return &launchd{opts: opts.withDefaults()} }

func (m *launchd) plistPath(name string) string {
	return filepath.Join(m.opts.Root, launchDaemonsDir, name+".plist")
}

func (m *launchd) logDir() string { return filepath.Join(m.opts.Root, launchdLogDir) }

func (m *launchd) run(ctx context.Context, args ...string) ([]byte, error) {
	return m.opts.Runner(ctx, "launchctl", args...)
}

// Install writes the plist and the log directory. Loading happens in
// Start, because bootstrapping a RunAtLoad daemon starts it.
func (m *launchd) Install(_ context.Context, s Service) ([]string, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	path := m.plistPath(s.Name)
	for _, dir := range []string{filepath.Dir(path), m.logDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // system directories are world-readable by convention
			return nil, fmt.Errorf("svc: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(RenderLaunchdPlist(s, m.logDir())), 0o644); err != nil { //nolint:gosec // plists are world-readable by convention and contain no secrets
		return nil, fmt.Errorf("svc: write plist: %w", err)
	}
	m.opts.Logger.Info("launchd daemon installed", "plist", path)
	return []string{path}, nil
}

// Start bootstraps the daemon into the system domain, or kickstarts it
// when it is already loaded.
func (m *launchd) Start(ctx context.Context, name string) error {
	if err := validName(name); err != nil {
		return err
	}
	if _, err := m.run(ctx, "bootstrap", "system", m.plistPath(name)); err != nil {
		if _, kerr := m.run(ctx, "kickstart", "-k", "system/"+name); kerr != nil {
			return errors.Join(err, kerr)
		}
	}
	return nil
}

// Stop unloads the daemon until the next Start or reboot.
func (m *launchd) Stop(ctx context.Context, name string) error {
	if err := validName(name); err != nil {
		return err
	}
	_, err := m.run(ctx, "bootout", "system/"+name)
	return err
}

// Uninstall unloads the daemon if loaded and removes the plist.
func (m *launchd) Uninstall(ctx context.Context, name string) error {
	if err := validName(name); err != nil {
		return err
	}
	path := m.plistPath(name)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := m.run(ctx, "bootout", "system/"+name); err != nil {
		m.opts.Logger.Debug("launchctl bootout failed, removing the plist anyway", "label", name, "err", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("svc: remove plist: %w", err)
	}
	return nil
}

// Status is Absent without a plist, Running when launchctl prints the
// job in state running, Stopped otherwise.
func (m *launchd) Status(ctx context.Context, name string) (State, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	if _, err := os.Stat(m.plistPath(name)); errors.Is(err, os.ErrNotExist) {
		return Absent, nil
	} else if err != nil {
		return "", fmt.Errorf("svc: %w", err)
	}
	out, err := m.run(ctx, "print", "system/"+name)
	if err != nil {
		return Stopped, nil
	}
	if strings.Contains(string(out), "state = running") {
		return Running, nil
	}
	return Stopped, nil
}

func (m *launchd) Logs(name string) string {
	return "tail -f " + path.Join(launchdLogDir, name+".log")
}

// RenderLaunchdPlist renders the LaunchDaemons plist for s: run at load,
// keep alive unless it exited cleanly, output to logDir/<name>.log.
func RenderLaunchdPlist(s Service, logDir string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	fmt.Fprintf(&b, "\t<key>Label</key>\n\t<string>%s</string>\n", xmlText(s.Name))
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range append([]string{s.Exec}, s.Args...) {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", xmlText(a))
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	b.WriteString("\t<key>ThrottleInterval</key>\n\t<integer>2</integer>\n")
	log := path.Join(logDir, s.Name+".log") // a macOS path, whatever the host
	fmt.Fprintf(&b, "\t<key>StandardOutPath</key>\n\t<string>%s</string>\n", xmlText(log))
	fmt.Fprintf(&b, "\t<key>StandardErrorPath</key>\n\t<string>%s</string>\n", xmlText(log))
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
