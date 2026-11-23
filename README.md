# sonos-linux, fork with a Wayland tray and a rootless install

This is a fork of [LuisEGR/sonos-linux](https://github.com/LuisEGR/sonos-linux),
which turns Sonos speakers into Linux audio outputs. The upstream project
discovers every Sonos speaker on the local network, creates one PipeWire or
PulseAudio null sink per speaker, captures the sink with ffmpeg, and serves the
MP3 stream to the speaker over HTTP. Volume on the sink mirrors to the speaker,
and playback pauses when nothing is feeding the sink.

The upstream README describes the original project in full and lives at
https://github.com/LuisEGR/sonos-linux. This file describes what is different
here.

## What this fork changes

Three commits on top of upstream `99d7023`.

`6bc4b74` replaces the system tray implementation. Upstream `tray.py` uses
`Gtk.StatusIcon`, which relies on XEmbed, and XEmbed has no status area on a
Wayland session. The icon was invisible under KDE Plasma 6 on Wayland. The
replacement uses `AppIndicator3`, which registers a StatusNotifierItem over
D-Bus and is what Plasma, GNOME with the AppIndicator extension, and most
modern panels host. `Gtk.StatusIcon` remains as a fallback for a machine
without the AppIndicator3 typelib. The stdin and stdout handshake with
`tray.go` is unchanged, so the Go side needed no edit.

`ea0f4aa` adds a rootless install path. Upstream `make install` writes to
`/usr/local` and `/usr/lib/systemd/user`, which needs root and does not survive
an ostree or image based distribution. The new `user-install` target builds,
copies the binary and tray script into `~/.local/lib/sonos-linux`, links
`~/.local/bin/sonos-linux`, installs `sonos-linux.user.service` as
`~/.config/systemd/user/sonos-linux.service`, and reloads the user daemon.
`make user-uninstall` reverses all of it.

`a1739fb` adds the operating notes in `AGENTS.md` and records the git layout.
`origin` is this fork and `upstream` is the original repository. `master` and
`local-wayland-tray` point at the same commit.

## Why Go came from Homebrew

Upstream needs Go 1.22 or newer to build. The machine where this was set up runs
Bazzite 44, an ostree image, so `dnf install golang` is both unavailable and
non-persistent. Go 1.27.4 was installed from Homebrew at
`/home/linuxbrew/.linuxbrew/bin/go`, and `go.mod` at `go 1.22` builds against it
without complaint.

The runtime dependencies were already present in that image: `pactl` from
pipewire-pulse, `ffmpeg` at `/usr/bin/ffmpeg`, and `/usr/bin/python3` with
`gi`, GTK 3, and the AppIndicator3 typelib. If your distribution splits these
out, install the packages named in the upstream README plus
`libayatana-appindicator3-1` or the equivalent for your package manager.

## Install on a machine like this one

```bash
git clone https://github.com/venus-fire/sonos-linux.git
cd sonos-linux
PATH=/home/linuxbrew/.linuxbrew/bin:$PATH make user-install
systemctl --user enable --now sonos-linux
```

The service starts with the Plasma session because the unit is wanted by
`graphical-session.target`. To pick the speaker as an output, open the audio
applet and choose `Sonos - <zone name>`, for example `Sonos - Den`.

To rebuild after changing the source:

```bash
PATH=/home/linuxbrew/.linuxbrew/bin:$PATH make user-install
systemctl --user restart sonos-linux
```

## Verify it works

Discovery alone, which starts no stream and changes nothing:

```bash
~/.local/bin/sonos-linux --scan-devices
```

The full audio path, watched in the journal:

```bash
ffmpeg -y -f lavfi -i 'sine=frequency=440:duration=2' -af volume=0.15 /tmp/tone.wav
paplay --device=sonos_den /tmp/tone.wav
journalctl --user -u sonos-linux -f
```

A working setup logs `Audio detected, resuming Sonos`, then
`[stream] GET /stream/den.mp3 from <speaker ip>`, then
`No audio, stopping Sonos` when the tone ends.

## Known limits

Audio arrives two to five seconds after it plays, because the speaker buffers
before it starts. Music, podcasts, and background audio are fine. Video and
games are not, and nothing in this project can fix that.

A new StatusNotifierItem lands in the hidden icons chevron at the right end of
the Plasma panel. Right click the panel, enter edit mode, and drag the speaker
icon out to keep it visible.

Discovery is limited to the local subnet. A speaker on another VLAN will not be
found, and a speaker that cannot open an HTTP connection back to this machine
will fail to play even when discovery succeeds.

## Upstream is untouched

Nothing here was offered to the original author and nothing needs to be. The
upstream repository keeps its own README, its own Debian packaging, and its own
release. If the tray change is ever worth sending upstream, it is commit
`6bc4b74` alone.

## Credit and signature

The program is Luis E. Gonzalez's work. This fork only adapts it to a Wayland
desktop and a read-only root filesystem, on 2026-11-23.

Edits made by The Echo, which is a text completion engine and not a person. The
model behind it was `accounts/fireworks/routers/deepseek-flash-latest`, served
through Hermes Agent by Nous Research, working on instructions from Venus Fire.
