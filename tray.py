#!/usr/bin/env python3
"""System tray icon for sonos-linux. Reads JSON from stdin, prints commands to stdout.

Uses AppIndicator3 (StatusNotifierItem over D-Bus) so the icon shows up on
Wayland desktops such as KDE Plasma 6, where Gtk.StatusIcon is invisible.
Falls back to Gtk.StatusIcon when AppIndicator3 is not installed.
"""

import json
import signal
import sys

import gi
gi.require_version("Gtk", "3.0")
from gi.repository import Gtk, GLib

try:
    gi.require_version("AppIndicator3", "0.1")
    from gi.repository import AppIndicator3 as AppIndicator
    HAS_INDICATOR = True
except (ValueError, ImportError):
    HAS_INDICATOR = False

ICON_NAME = "audio-speakers-symbolic"


class SonosTray:
    def __init__(self, data):
        self.data = data

        self.menu = self.build_menu()
        if HAS_INDICATOR:
            self.indicator = AppIndicator.Indicator.new(
                "sonos-linux", ICON_NAME, AppIndicator.IndicatorCategory.HARDWARE)
            self.indicator.set_title("Sonos Stream")
            self.indicator.set_status(AppIndicator.IndicatorStatus.ACTIVE)
            self.indicator.set_menu(self.menu)
            return

        self.icon = Gtk.StatusIcon()
        self.icon.set_from_icon_name(ICON_NAME)
        self.icon.set_tooltip_text("Sonos Stream")
        self.icon.set_visible(True)
        self.icon.connect("popup-menu", self.on_popup)
        self.icon.connect("activate", self.on_activate)

    def build_menu(self):
        menu = Gtk.Menu()

        groups = self.data.get("groups", [])
        active_count = sum(1 for g in groups if g.get("active"))

        # Status
        status = Gtk.MenuItem(label=f"Streaming to {active_count} system(s)")
        status.set_sensitive(False)
        menu.append(status)
        menu.append(Gtk.SeparatorMenuItem())

        # Level 1: Groups (systems)
        for group in groups:
            group_name = group.get("name", "Unknown")
            active = group.get("active", False)

            if active:
                # Use a check menu item to show selection
                group_item = Gtk.CheckMenuItem(label=group_name)
                group_item.set_active(True)
                group_item.set_sensitive(True)
                # Prevent toggling
                group_item.connect("toggled", lambda w: w.set_active(True))
            else:
                group_item = Gtk.MenuItem(label=group_name)

            # Level 2: Devices in this group
            devices_menu = Gtk.Menu()
            for dev in group.get("devices", []):
                dev_label = dev.get("name", "Unknown")
                model = dev.get("model", "")
                if model:
                    dev_label += f" ({model})"
                if dev.get("is_coordinator"):
                    dev_label += " ★"

                dev_item = Gtk.MenuItem(label=dev_label)

                # Level 3: Device details
                details_menu = Gtk.Menu()
                for key, val in [
                    ("IP", dev.get("ip")),
                    ("Model", f"{dev.get('model')} ({dev.get('model_number')})"),
                    ("UUID", dev.get("uuid")),
                    ("Serial", dev.get("serial")),
                    ("Software", dev.get("software")),
                    ("Hardware", dev.get("hardware")),
                    ("Coordinator", str(dev.get("is_coordinator", False))),
                    ("Invisible", str(dev.get("invisible", False))),
                    ("Can play", str(dev.get("can_play", False))),
                ]:
                    info = Gtk.MenuItem(label=f"{key}: {val}")
                    info.set_sensitive(False)
                    details_menu.append(info)

                dev_item.set_submenu(details_menu)
                devices_menu.append(dev_item)

            group_item.set_submenu(devices_menu)
            menu.append(group_item)

        menu.append(Gtk.SeparatorMenuItem())

        # Quit
        quit_item = Gtk.MenuItem(label="Quit")
        quit_item.connect("activate", self.on_quit)
        menu.append(quit_item)

        menu.show_all()
        return menu

    def on_popup(self, icon, button, time):
        self.menu.popup(None, None, Gtk.StatusIcon.position_menu, icon, button, time)

    def on_activate(self, icon):
        self.menu.popup(None, None, Gtk.StatusIcon.position_menu, icon, 1,
                        Gtk.get_current_event_time())

    def on_quit(self, _):
        print("QUIT", flush=True)
        Gtk.main_quit()


def main():
    line = sys.stdin.readline().strip()
    if not line:
        sys.exit(1)
    data = json.loads(line)

    signal.signal(signal.SIGTERM, lambda *_: Gtk.main_quit())
    signal.signal(signal.SIGINT, lambda *_: Gtk.main_quit())

    # Exit when parent dies (stdin closes)
    channel = GLib.IOChannel.unix_new(sys.stdin.fileno())
    GLib.io_add_watch(channel, GLib.PRIORITY_DEFAULT, GLib.IOCondition.HUP,
                      lambda *_: (Gtk.main_quit(), False)[-1])

    SonosTray(data)
    Gtk.main()


if __name__ == "__main__":
    main()
