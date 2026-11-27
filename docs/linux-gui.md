# Twinnel's GUI on Linux

The same app (tray icon, panel, main window) built from `cmd/app` for Linux. MyGo's Linux backend
is pure Go too (GTK 3, libayatana-appindicator and friends are loaded at run time with purego), so
`CGO_ENABLED=0 GOOS=linux GOARCH=amd64|arm64 go build ./cmd/app` works from any host, including
GitHub's ubuntu runner and a Mac. Nothing of this has run on a Linux desktop yet: see the test plan
in the branch's HANDOFF.md.

Targets: Arch + Hyprland (Wayland, the tray hosted by a quickshell bar with StatusNotifierItem
support) and KDE Plasma first; GNOME second (needs the AppIndicator extension); X11 through GTK's
X11 backend.

## What the Linux build does

| Area | macOS | Linux |
|---|---|---|
| Tray icon | NSStatusItem, template PNG, speed text beside it | StatusNotifierItem through libayatana-appindicator; the same monochrome PNG, whitened by MyGo while the portal's color scheme is dark. No speed text (`plat.trayTitle` off; 设置 hides the 菜单栏 card) |
| Tray click | left: flyout; right: menu | the icon always has a menu (显示面板 / 打开主窗口 / 退出): KDE opens it on a left click. Its activation (left click on quickshell/waybar, double click on GNOME) opens the panel |
| Panel | NSPopover flyout under the icon | a Panel window (`panel_linux.go`): a wlr-layer-shell surface on Hyprland/KDE/Sway, at a corner of the primary display's work area (top-right; bottom-right on KDE; `TWINNEL_PANEL_POSITION=top-left\|top-right\|bottom-left\|bottom-right\|center\|pointer`, pointer on X11 only). It fits its content's height, closes on losing the keyboard, Escape, or a second activation |
| No tray host | — | GNOME without AppIndicator, or a bar without a tray: the main window opens with a toast that says so (GNOME: turn on the extension), and closing it quits. No libayatana at all: same |
| Title bar | hidden-inset, traffic lights, toggle after them | hidden; the desktop's own window buttons (gtk-decoration-layout) over the app's 52 px toolbar band, centered in it (`TitleBarHeight`); none where the compositor decorates (KWin's title bar sits above; Hyprland draws none). The sidebar toggle and the toolbar keep clear of the buttons on either side; no traffic-light math (`plat.trafficLights`) |
| Shortcuts | ⌘1…⌘9, ⌘, ⌘[ ⌘W ⌃⌘S, ⌥⌘↑/↓ | MyGo maps `ui.Cmd` to Ctrl: Ctrl+1…9, Ctrl+, Ctrl+[ Ctrl+W, Ctrl+Alt+↑/↓; the sidebar toggles with F9 (GNOME's convention). No app menu bar (macOS only) |
| Look | macOS system grays, accent, vibrancy sidebar | libadwaita's palette (`adwaitaTheme`), opaque sidebar, the portal's accent-color, color-scheme and contrast through MyGo's Preferences |
| Fonts | SF Pro / PingFang, SF Mono | the desktop's `gtk-font-name` (Cantarell, Noto Sans, Inter…) resolved by fontconfig, CJK by fontconfig's fallback (Noto Sans CJK); `monospace` is fontconfig's monospace. No SF names anywhere in code |
| Dock / activation policy | 在程序坞中显示 | not shown (`plat.dock` off); `SetActivationPolicy` is a no-op |
| 登录时打开 | SMAppService, needs the bundle | an XDG autostart entry (`~/.config/autostart/<id>.desktop`, MyGo), offered when the app is not a `go run` build |
| Helper install | osascript administrator dialog → LaunchDaemon | `pkexec` (polkit; the session's authentication agent asks) → `twinnel helper install --as-root`, which writes `/etc/systemd/system/twinnel-helper.service`, `/usr/local/libexec/twinnel-helper`, `/var/lib/twinnel`. argv words go to pkexec as they are, no shell. 126 = dialog dismissed, 127 = refused or no agent |
| Helper detection | the plist / PrivilegedHelperTools binary | `/usr/local/libexec/twinnel-helper` or the unit file |
| Socket group | admin | `sudo`, else `wheel`, else a new `twinnel` group. `helper install` and `helper upgrade` add the person installing (`PKEXEC_UID`, `SUDO_UID`, or the user running it) with `usermod -a -G` when the group database does not list them; upgrade does it even with a current helper. The app tells a refused socket apart: "not in group X" with 加入 (runs `helper upgrade`), or "added: log out and back in" (`helpergroup.go`), instead of "not running" |
| Helper log | open in Console | the log file when readable, else `journalctl --unit twinnel-helper.service --pager-end` in a terminal (`$TERMINAL`, xdg-terminal-exec, kitty, foot, ghostty, alacritty, wezterm, konsole, kgx, gnome-terminal, xfce4-terminal, xterm) |
| Reveal a file | Finder | `org.freedesktop.FileManager1.ShowItems`, else `xdg-open` of the folder (MyGo) |
| Notifications | packaged app only | `org.freedesktop.Notifications` (MyGo) |
| 完全移除 | trash Twinnel.app | the settings go; the binary stays (it belongs to whatever installed it) and the list says so |
| Wording | Finder, Keychain, LaunchDaemon, this Mac | `core/i18n/desktop.go`: 文件管理器, 密钥文件, systemd, 这台电脑 / this computer (applied at init on Linux only) |
| Update check proxy | scutil --proxy | `http_proxy`/`https_proxy` from the environment |

## The audit: every macOS assumption in cmd/app

Each line: where, what it assumed, what Linux does now.

- `main.go` `ready`: `log.Fatal` when the tray cannot be made → on Linux the window stands in (`trayMissing`).
- `main.go` `appMenu` (⌘ menus, Services, Hide): built on darwin only; nothing to do.
- `main.go` `togglePanel`: `NewFlyout{Tray, Popover}` → `openPanelWindow` (above).
- `main.go` `openMain`: `TitleBarHiddenInset` + `VibrancySidebar` → `plat.titleBar` hidden, no material, `TitleBarHeight: 52`.
- `dock.go`: activation policy, Dock menu, `App.Hide` → `plat.dock` false; all of it is skipped.
- `mygo.json` `LSUIElement`: macOS only; Linux needs nothing (no Dock).
- `icons.go` `trayIcon`: 41×36 template PNG at 2x. Panels scale it to their height; MyGo whitens it while the desktop prefers dark. A light desktop with a dark bar shows it dark on dark (see open issues).
- `trayspeed.go`: the speed beside the icon is an NSStatusItem title → off (`plat.trayTitle`).
- `sidebar.go` `trafficLightsMid`, `railWidth`, `sideToggleX`: measured the traffic lights → branch on `plat.trafficLights`; Linux centers on the title bar height and clears `TitleBar().Left`.
- `sidebar.go` ⌃⌘S → F9 on Linux.
- `window.go` toolbar: padded only for the left lights → also clears `TitleBar().Right` (GNOME's close button).
- `window.go` sidebar background stand-in `#ebebeb`/`#2a2a2c`: close to Adwaita's sidebar; kept.
- `platform.go` `systemTheme`: macOS system colors → `adwaitaTheme` on Linux.
- `platform.go` Linux style said "the system's own title bar" and "no flyout on Linux" → hidden title bar, panel window.
- `widgets.go` `mono` ("SF Mono"): uses the generic `monospace`; comment only.
- `helper.go` `helperInstalledPaths` (`/Library/...`) → Linux paths (`helper_linux.go`).
- `helpermgmt.go` `helperLogFile` (`store.DefaultDir`) → `/var/lib/twinnel/logs/helper.log`; `openHelperLog` (Console) → file or journal.
- `helpermgmt.go` `appBundlePath`/`appBundled` (.app, SMAppService) → `appInstalled` on Linux; no trash of the binary.
- `onboarding.go` `cliPath`: bundle `Contents/Resources/bin`, then next to the executable, then `$PATH`: the Linux tarball's layout (twinnel next to the app) works; `TWINNEL_CLI` overrides.
- `onboarding.go`/`cmd/cli/helper_install.go`: osascript elevation → per-platform `elevation` (pkexec on Linux).
- `installplan.go`: knew launchctl lines; now words systemctl enable/restart/disable too. `groupadd` shows as the command.
- `leavepage.go` `launchAgents`/`bootoutAgent` (`~/Library/LaunchAgents`, launchctl): finds nothing on Linux, so never runs.
- `legacyprefs.go` (`~/Library/Application Support/duet`): no such folder on Linux.
- `aboutpage.go`/`leavepage.go`/`takeoverview.go` 在 Finder 中显示: MyGo's ShowItemInFolder; the label is 在文件管理器中显示.
- `notify.go`: MyGo's notifications; no bundle requirement on Linux.
- `platform_darwin.go` `systemProxy` (scutil) → environment (`platform_linux.go`).
- `lang.go`: system languages through MyGo's locale (LANGUAGE/LC_*/LANG on Linux).
- `shotguard.go`: a scratch HOME did not move `XDG_CONFIG_HOME` → refused unless unset or under HOME.
- i18n texts naming macOS things: listed in `linuxShown` (`core/i18n/desktop_test.go`); the Linux table rewrites them.
- Not changed, macOS only and hidden on Linux: 在程序坞中显示, the Login Items approval text, the app menu's 显示 menu, LaunchAgent inventory rows.

## Runtime libraries

MyGo loads these with dlopen (names from `internal/linux/ffi.go` of MyGo v0.10.0):

| Needed for | Library | Arch | Debian/Ubuntu | Fedora |
|---|---|---|---|---|
| windows, menus | GTK 3 (gtk, gdk, pango, cairo, gdk-pixbuf, glib/gio) | `gtk3` | `libgtk-3-0` (`libgtk-3-0t64` on 24.04+) | `gtk3` |
| tray icon | libayatana-appindicator3 (or libappindicator3), dbusmenu | `libayatana-appindicator` | `libayatana-appindicator3-1` | `libayatana-appindicator-gtk3` |
| panel as a layer surface (Hyprland, KDE, Sway) | gtk-layer-shell (optional: a plain window without it) | `gtk-layer-shell` | `libgtk-layer-shell0` | `gtk-layer-shell` |
| fonts | fontconfig, harfbuzz, a UI font and a CJK font | `fontconfig noto-fonts noto-fonts-cjk` | `fontconfig fonts-noto-core fonts-noto-cjk` | `fontconfig google-noto-sans-fonts google-noto-sans-cjk-fonts` |
| GL (MyGo's renderer, when used; probed, falls back) | libepoxy, Mesa EGL/GL | `libepoxy mesa` | `libepoxy0 libegl1 libgl1` | `libepoxy mesa-libEGL` |
| Wayland | libwayland-client | `wayland` | `libwayland-client0` | `libwayland-client` |
| X11 | libX11, libXtst | `libx11 libxtst` | `libx11-6 libxtst6` | `libX11 libXtst` |
| web pages (not used by Twinnel's native UI; loaded only for a web view) | WebKitGTK 4.1 | `webkit2gtk-4.1` | `libwebkit2gtk-4.1-0` | `webkit2gtk4.1` |
| secrets (not used by the app) | libsecret | `libsecret` | `libsecret-1-0` | `libsecret` |
| helper install | polkit + an authentication agent | `polkit` + `hyprpolkitagent` (Hyprland) / built into Plasma | `policykit-1` / `pkexec` | `polkit` |
| journal, tray check | systemd (`journalctl`, `busctl`), or `gdbus`/`dbus-send` | `systemd` | `systemd` | `systemd` |

The packaging agent (feat/linux-packaging) owns the .desktop file, icons, install.sh, AppImage and
the release job; this table is what they need for the dependency lists.

## Screenshots on Linux

`TWINNEL_SHOTS` renders with `Window.CapturePage`, which for native Content windows is MyGo's own
renderer (`content.go captureContent`), not the compositor: the same flow should work on Linux. GTK
still needs a display connection to start, so run it under `xvfb-run -a` or a headless compositor
(`weston --backend=headless`, `cage -d`), never on the owner's session. Untested here (no Linux
desktop on this Mac).

## Open issues

- The tray icon is a template PNG whitened by the portal's color scheme, not a themed symbolic icon
  name: a dark bar on a light desktop (common with Hyprland bars) shows a dark icon on dark. The
  symbolic SVGs in `assets/icons/tray/linux/` could be installed into the hicolor theme and named
  instead (needs MyGo to take an icon name, or the packaging to install them).
- The panel cannot sit under the icon: AppIndicator gives no position. On GNOME Wayland (no layer
  shell) Mutter centers it.
