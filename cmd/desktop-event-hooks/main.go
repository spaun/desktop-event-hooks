package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sync/errgroup"
)

var version = ""
var errHookNotFound = errors.New("hook not found")

const MODE_DEFAULT uint32 = 0
const MODE_DARK uint32 = 1
const MODE_LIGHT uint32 = 2

type hook struct {
	name    string
	path    string
	timeout time.Duration
}

func getVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()

	if !ok {
		return "dev"
	}

	v := info.Main.Version
	if v == "" || v == "(devel)" {
		v = "dev"
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				v = s.Value[:7]
				break
			}
		}
	}

	return v
}

func expandHomeDir(hooksDir string) (string, error) {
	if hooksDir == "~" || strings.HasPrefix(hooksDir, "~/") {
		homeDir, err := os.UserHomeDir()

		if err != nil {
			return hooksDir, err
		}

		hooksDir = filepath.Join(homeDir, hooksDir[1:])
	}

	return hooksDir, nil
}

func listenDarkMode(ctx context.Context, h hook) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("fail to connect to session bus: %w", err)
	}
	defer conn.Close()

	if err = conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/portal/desktop"),
		dbus.WithMatchInterface("org.freedesktop.portal.Settings"),
		dbus.WithMatchSender("org.freedesktop.portal.Desktop"),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		return fmt.Errorf("fail to add matching signals %w", err)
	}

	var currentMode uint32 = math.MaxUint32

	dbusCh := make(chan *dbus.Signal, 10)
	conn.Signal(dbusCh)

	for {
		select {
		case v := <-dbusCh:
			if len(v.Body) < 3 {
				continue
			}

			namespace := v.Body[0]
			key := v.Body[1]
			message, ok := v.Body[2].(dbus.Variant)

			if !ok || namespace != "org.freedesktop.appearance" || key != "color-scheme" {
				continue
			}

			mode, ok := message.Value().(uint32)

			if !ok || currentMode == mode {
				continue
			}

			currentMode = mode

			var modeName string

			switch mode {
			case MODE_DEFAULT:
				modeName = "default"
			case MODE_DARK:
				modeName = "dark"
			case MODE_LIGHT:
				modeName = "light"
			default:
				slog.Warn("unknown mode value", "mode", mode)
				continue
			}

			err := callHook(ctx, h, modeName)

			if errors.Is(err, errHookNotFound) {
				slog.Debug("hook not installed", "hook", h.name, "path", h.path)
			} else if err != nil {
				slog.Error("hook call failed", "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func callHook(ctx context.Context, h hook, args ...string) error {
	fileInfo, err := os.Stat(h.path)

	if errors.Is(err, fs.ErrNotExist) {
		return errHookNotFound
	}

	if err != nil {
		return fmt.Errorf("hook %s (%s): %w", h.name, h.path, err)
	}

	if fileInfo.Mode()&0100 == 0 {
		return fmt.Errorf("hook %s (%s): not executable", h.name, h.path)
	}

	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, h.path, args...)
	cmd.WaitDelay = min(h.timeout, 2*time.Second)
	out, err := cmd.CombinedOutput()

	if len(out) > 0 {
		slog.Info("hook output", "hook", h.name, "output", strings.TrimSpace(string(out)))
	}

	if err != nil {
		return fmt.Errorf("hook %s (%s): %w", h.name, h.path, err)
	}

	return nil
}

func run(hooksDir string, timeout time.Duration) error {
	slog.Info("starting desktop events listener", "hooksDir", hooksDir, "timeout", timeout)
	defer slog.Info("shutting down desktop events listener")

	hooksDir, err := expandHomeDir(hooksDir)
	if err != nil {
		return fmt.Errorf("fail to init hook paths: %w", err)
	}

	darkModeHook := hook{
		name:    "dark-mode",
		path:    filepath.Join(hooksDir, "dark-mode-hook"),
		timeout: timeout,
	}

	listeners := []func(context.Context) error{
		func(ctx context.Context) error { return listenDarkMode(ctx, darkModeHook) },
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	for _, l := range listeners {
		g.Go(func() error { return l(ctx) })
	}

	return g.Wait()
}

func main() {
	hooksDir := flag.String("hooks-path", "~/.local/hooks", "path to hooks directory")
	showVersion := flag.Bool("version", false, "version")
	timeout := flag.Duration("timeout", 10*time.Second, "hook execution timeout")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")

	flag.Parse()

	if *showVersion {
		fmt.Printf("Desktop event hooks %s\n", getVersion())
		os.Exit(0)
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level %q: %v\n", *logLevel, err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if err := run(*hooksDir, *timeout); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}
