# sonos-linux

Play Linux system audio on Sonos speakers. This is a fork of
[LuisEGR/sonos-linux](https://github.com/LuisEGR/sonos-linux), which is Luis E.
Gonzalez's work; that repository carries the full description of the program and
its history. What follows describes how this copy is set up and what it does
differently.

The daemon discovers every Sonos speaker on the local network over SSDP,
creates one PipeWire or PulseAudio null sink per speaker named `Sonos - <zone>`,
captures that sink with ffmpeg, and serves the encoded audio to the speaker over
HTTP. The speaker's own volume follows the sink slider, and playback pauses when
nothing is feeding the sink and resumes when audio returns. It runs as a systemd
user service with a tray menu showing each speaker and its details.

Audio arrives two to five seconds after it plays, because a Sonos buffers before
it starts. That is fine for music, podcasts and background audio, and wrong for
video and games. Nothing here can reduce it.

## What this fork changes

Three things: a tray that works on Wayland, an install that stays in the home
directory, and a lossless stream.

The tray runs on AppIndicator3 instead of `Gtk.StatusIcon`. Upstream's tray
helper draws an XEmbed icon, and XEmbed has no status area on Wayland, so the
icon was invisible. AppIndicator3 registers a StatusNotifierItem over D-Bus,
which Plasma hosts properly, and `Gtk.StatusIcon` stays as a fallback for hosts
without the typelib. The stdin and stdout handshake with `tray.go` is unchanged,
so the Go side needed no edit.

Installing is rootless. Upstream's `make install` writes to `/usr/local` and
`/usr/lib/systemd/user`, which needs root and does not survive an ostree or
image based distribution. The `user-install` target builds and copies everything
into the home directory instead. `make user-uninstall` reverses it.

The stream is lossless FLAC at 48 kHz instead of MP3 at 192 kbps. Upstream's
comment says MP3 is the only format Sonos accepts on a live stream, and that is
not true of this hardware. The speaker advertises `audio/flac` and plays a
chunked FLAC pipe, so the quality ceiling moved. Details and the measured cost
are under Codec below.

## Install

Building needs Go 1.22 or newer, `pactl` from pipewire-pulse, `ffmpeg`, and
`python3` with `gi`, GTK 3 and the AppIndicator3 typelib. On a distribution
whose Python is split into packages, the tray needs `python3-gi`,
`gir1.2-gtk-3.0` and `libayatana-appindicator3-1` or their equivalents.

Where Go is not in the base image, install it with Homebrew rather than a
system package manager, so the toolchain lives under the home directory:

```bash
git clone https://github.com/venus-fire/sonos-linux.git
cd sonos-linux
PATH=/home/linuxbrew/.linuxbrew/bin:$PATH make user-install
systemctl --user enable --now sonos-linux
```

That puts the binary and tray helper in `~/.local/lib/sonos-linux`, links
`~/.local/bin/sonos-linux`, and installs the unit as
`~/.config/systemd/user/sonos-linux.service`, wired to start with
`graphical-session.target`. Rebuilding after a source change is the same two
commands plus a restart:

```bash
PATH=/home/linuxbrew/.linuxbrew/bin:$PATH make user-install
systemctl --user restart sonos-linux
```

## Using it

Pick `Sonos - <zone>` in the audio applet and everything you play goes to that
speaker. The sink volume sets the speaker volume, and the tray menu lists each
speaker with its address, model, serial and firmware. Use Quit in that menu or
`systemctl --user stop sonos-linux` to stop it.

```bash
~/.local/bin/sonos-linux --scan-devices   # list speakers, start no stream
journalctl --user -u sonos-linux -f       # follow the log
```

To check the whole audio path rather than just discovery, play a short tone into
the sink and watch the journal:

```bash
ffmpeg -y -f lavfi -i 'sine=frequency=440:duration=2' -af volume=0.15 /tmp/tone.wav
paplay --device=sonos_den /tmp/tone.wav
```

A working setup logs `Audio detected, resuming Sonos`, then
`[stream] GET /stream/den.flac from <speaker ip>`, then
`No audio, stopping Sonos` when the tone ends.

## Codec

The stream is MP3 at 320 kbps and 48 kHz by default. 320k keeps the monitor's
own rate, so nothing is resampled, and the bitrate is high enough that the loss
is not the thing you hear.

FLAC was the default for a while, and this speaker does not hold it. The Beam
accepts `audio/flac` and starts playing, then drops the stream every ten to
thirty seconds; measured over forty seconds of continuous audio, MP3-320 played
through without a single drop, including a track change in the middle. Lossless
is available with `SONOS_CODEC=flac` for a speaker that behaves better, or for
another Sonos model.

Set `SONOS_CODEC` to choose. `mp3-320` is the default, `flac` is lossless,
`mp3` reproduces the original upstream 192k path at 44.1 kHz, and `wav` is
uncompressed. An unrecognised value logs a warning and falls back to the
default rather than silently doing nothing.

```bash
systemctl --user edit sonos-linux
# [Service]
# Environment=SONOS_CODEC=mp3-320
```

Bitrates below are measured on 60 seconds of 44.1 kHz 16-bit stereo music. The
startup log line `Stream codec: mp3-320 (audio/mpeg)` names the one that is
live.

| value | rate | bitrate | note |
| --- | --- | --- | --- |
| mp3-320 | 48000 | 320 kbps | default, stable |
| flac | 48000 | 968 kbps | lossless, drops on this speaker |
| mp3 | 44100 | 192 kbps | original upstream path |
| wav | 48000 | 1536 kbps | uncompressed |

Adding a format means adding a `Codec` value in `audio/codec.go` with its ffmpeg
arguments, path extension and MIME type, and confirming the speaker lists that
type in `ConnectionManager.GetProtocolInfo`. `AGENTS.md` carries the query.

## Limits

A new StatusNotifierItem lands in the hidden icons chevron at the right end of
the Plasma panel. Right click the panel, enter edit mode, and drag the speaker
icon out once to keep it visible.

Discovery is limited to the local subnet. A speaker on another VLAN will not be
found, and a speaker that cannot open an HTTP connection back to this machine
will fail to play even when discovery succeeds.

## Upstream

Nothing here was offered to the original author and nothing needs to be. Their
repository keeps its own README, Debian packaging and releases. If the tray
change is ever worth proposing upstream, it is the single commit `6bc4b74`; the
rest is specific to this host.

`origin` is this fork and `upstream` is the original repository. `master` and
`local-wayland-tray` point at the same commit, and `AGENTS.md` holds the
operating notes: pins, rebuild commands, and the failure playbook.

---

Adapted to a Wayland desktop, a read-only root filesystem and a lossless stream
on 2026-11-23. Edits made by The Echo, which is a text completion engine and not
a person. The model behind it was `accounts/fireworks/routers/deepseek-flash-latest`,
served through Hermes Agent by Nous Research, working on instructions from Venus
Fire.
