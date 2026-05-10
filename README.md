# desktop-event-hooks

A lightweight daemon that listens for desktop events via D-Bus and calls user-defined hook scripts in response.

## Events

| Event | Hook script | Arguments |
|-------|-------------|-----------|
| Dark/light mode changed | `dark-mode-hook` | `dark`, `light`, or `default` |

The daemon subscribes to `org.freedesktop.portal.Settings` on the session bus and fires the hook whenever the `org.freedesktop.appearance color-scheme` setting changes.

## Hook scripts

Hooks live in a directory (default: `~/.local/hooks`). Each hook is an executable file named after the event. The daemon passes the new state as the first argument.

Example `~/.local/hooks/dark-mode-hook`:

```sh
#!/bin/sh
# $1 is "dark", "light", or "default"
case "$1" in
  dark)    gsettings set org.gnome.desktop.interface gtk-theme 'Adwaita-dark' ;;
  light|default) gsettings set org.gnome.desktop.interface gtk-theme 'Adwaita' ;;
esac
```

Make it executable:

```sh
chmod +x ~/.local/hooks/dark-mode-hook
```

## Build & install

```sh
make build    # produces bin/desktop-event-hooks
make install  # installs to $GOPATH/bin
```

## Usage

```
desktop-event-hooks [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-hooks-path` | `~/.local/hooks` | Directory containing hook scripts |
| `-timeout` | `10s` | Hook execution timeout |
| `-log-level` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `-version` | | Print version and exit |

## Running as a systemd user service

```sh
cp systemd/desktop-event-hooks.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now desktop-event-hooks
```

The service file expects the binary at `~/.local/bin/desktop-event-hooks` (i.e. `make install` with `GOBIN=~/.local/bin`).

## Requirements

- Linux with D-Bus session bus
- A desktop environment that implements `org.freedesktop.portal.Settings` (GNOME, KDE, etc.)
