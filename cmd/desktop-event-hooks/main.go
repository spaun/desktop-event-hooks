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
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sync/errgroup"
)

var version = "dev"

// 0 - default (usually light), 1 - dark, 2 - light
const MODE_DARK uint32 = 1

type hook struct {
	name string
	path string
}

type hooksStruct struct {
	darkModeChanged hook
}

func (hooks *hooksStruct) init(hooksDir string) error {
	if strings.HasPrefix(hooksDir, "~/") {
		homeDir, err := os.UserHomeDir()

		if err != nil {
			return err
		}

		hooksDir = filepath.Join(homeDir, hooksDir[2:])
	}

	hooks.darkModeChanged = hook{
		name: "dark-mode",
		path: filepath.Join(hooksDir, "dark-mode-hook"),
	}

	return nil
}

func (hooks *hooksStruct) listenDarkMode(ctx context.Context) error {
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

			modeName := "light"

			if mode == MODE_DARK {
				modeName = "dark"
			}

			err := callHook(hooks.darkModeChanged, modeName)

			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Error("hook call failed", "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func callHook(h hook, args ...string) error {
	fileInfo, err := os.Stat(h.path)
	if err != nil {
		return fmt.Errorf("hook %s (%s): %w", h.name, h.path, err)
	}
	if fileInfo.Mode()&0100 == 0 {
		return fmt.Errorf("hook %s (%s): not executable", h.name, h.path)
	}

	cmd := exec.Command(h.path, args...)
	out, err := cmd.CombinedOutput()

	if len(out) > 0 {
		slog.Info("hook output", "hook", h.name, "output", strings.TrimSpace(string(out)))
	}

	if err != nil {
		return fmt.Errorf("hook %s (%s): %w", h.name, h.path, err)
	}

	return nil
}

func run() error {
	slog.Info("starting desktop events listener")
	defer slog.Info("shutting down desktop events listener")

	hooksDir := flag.String("hooks-path", "~/.local/hooks", "path to hooks directory")

	flag.Parse()

	var hooks hooksStruct
	if err := hooks.init(*hooksDir); err != nil {
		return fmt.Errorf("fail to init hook paths: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return hooks.listenDarkMode(ctx) })

	return g.Wait()
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}
