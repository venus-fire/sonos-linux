# AGENTS.md

Sonos as a Linux audio output. Upstream is https://github.com/LuisEGR/sonos-linux,
cloned to this directory and installed rootless.

## What it is

A Go daemon that discovers Sonos speakers over SSDP, creates one PulseAudio null
sink per speaker ("Sonos - Den"), captures that sink's monitor with ffmpeg, MP3
encodes it, and serves it over HTTP to the speaker. It also polls the sink volume
every 500 ms and pushes it to the Sonos, and pauses the speaker when no client is
playing into the sink.

Speaker buffering means a 2 to 5 second delay. Music and podcasts are fine; video
and games are not.

## State on this host

- Discovered device: Sonos Beam (S14), zone name "Den", 10.0.0.161, RINCON_949F3ED1CB2201400.
- Installed binary and tray: `~/.local/lib/sonos-linux/{sonos-linux,tray.py}`.
- Launcher symlink: `~/.local/bin/sonos-linux`.
- Unit: `~/.config/systemd/user/sonos-linux.service`, enabled in
  `graphical-session.target.wants`, so it starts with the Plasma session.
- Sink: `sonos_den`, description "Sonos - Den".

## Toolchain and why

Upstream wants Go 1.22+ to build. There is no Go in the base image, and the image
is ostree so `dnf` is not an option. Go 1.27.4 comes from Homebrew
(`/home/linuxbrew/.linuxbrew/bin/go`); `go.mod` still says 1.22, which is fine.

Runtime deps are all present in the base image: `pactl` (pipewire-pulse),
`ffmpeg` at /usr/bin/ffmpeg, `/usr/bin/python3` with `gi` + GTK 3 + the
AppIndicator3 typelib. Note the agent's own `python3` on PATH is Hermes' bundled
interpreter and has no `gi`, so the tray only works when `python3` resolves to
`/usr/bin/python3`. The unit sets `Environment=PATH=/usr/local/bin:/usr/bin:/bin`
for exactly this reason.

## Codec

`SONOS_CODEC` selects the wire format, resolved by `audio.CodecFromEnv()` in
`audio/codec.go`. Default is `flac`: 48 kHz, lossless, about 968 kbps of stereo,
and no resample because 48 kHz is what the monitor runs at. `mp3-320` keeps 48
kHz at 320 kbps, `mp3` reproduces the original upstream 192k/44.1k path, and
`wav` is uncompressed at 1536 kbps. The device advertises all of them in
`ConnectionManager.GetProtocolInfo`; `audio/flac` was proven live against the
Beam here.

The content type comes from the codec, so the stream path extension and the
header always agree. If you add a codec, add its `Codec` value with `Ext`,
`ContentType` and ffmpeg args, and confirm the speaker lists that MIME type:

```
curl -s -X POST http://<speaker>:1400/MediaRenderer/ConnectionManager/Control \
  -H 'SOAPAction: "urn:schemas-upnp-org:service:ConnectionManager:1#GetProtocolInfo"' \
  -H 'Content-Type: text/xml; charset="utf-8"' \
  --data '<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetProtocolInfo xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"/></s:Body></s:Envelope>'
```

To switch on this host without editing the repo, `systemctl --user edit
sonos-linux` and add `Environment=SONOS_CODEC=mp3` under `[Service]`. Log line
`Stream codec: flac (audio/flac)` at startup is the confirmation.

## Rebuild and reinstall

```
cd ~/Projects/sonos-linux
PATH=/home/linuxbrew/.linuxbrew/bin:$PATH make user-install
systemctl --user restart sonos-linux
```

`make user-install` (added locally, not upstream) builds, copies the binary and
tray script to `~/.local/lib/sonos-linux`, links `~/.local/bin/sonos-linux`, and
installs `sonos-linux.user.service` as `~/.config/systemd/user/sonos-linux.service`.
`make user-uninstall` reverses it. The upstream `install`/`enable` targets write to
`/usr/local` and `/usr/lib/systemd/user`, so they need root and do not work here.

## Verification

```
systemctl --user status sonos-linux
pactl list short sinks | grep sonos
~/.local/bin/sonos-linux --scan-devices     # discovery only, starts no stream
journalctl --user -u sonos-linux -f          # log lines: "Audio detected, resuming Sonos"
```

To prove the audio path end to end, play a short clip into the sink and watch the
log:

```
ffmpeg -y -f lavfi -i 'sine=frequency=440:duration=2' -af volume=0.15 /tmp/tone.wav
paplay --device=sonos_den /tmp/tone.wav
```

Expected: "Audio detected, resuming Sonos", `[stream] GET /stream/den.mp3 from
10.0.0.161`, then "No audio, stopping Sonos" when it ends, and the speaker's
`CurrentTransportState` goes PLAYING then PAUSED_PLAYBACK.

## Local modification: the tray

Upstream `tray.py` uses `Gtk.StatusIcon`, which is XEmbed-based and therefore
invisible on KDE Plasma 6 Wayland. Commit `6bc4b74` switches to `AppIndicator3`
(StatusNotifierItem over D-Bus) and keeps `Gtk.StatusIcon` as a fallback when the
typelib is missing. The stdin/stdout contract with `tray.go` is unchanged. The
indicator registers as `org/ayatana/NotificationItem/sonos_linux`; Plasma drops
new items into the hidden-icons chevron at the right end of the panel, so it has
to be dragged out once through panel edit mode.

Git layout: `origin` is the fork `venus-fire/sonos-linux`, `upstream` is
`LuisEGR/sonos-linux`. `master` carries the two local commits and is the fork's
default branch; `local-wayland-tray` points at the same commit. Do not
fast-forward `master` from `upstream` without carrying these commits forward,
because upstream ships the XEmbed version.

## The sink and the default output

The service never sets the default sink, and it should not need to. But
WirePlumber restores a remembered default *by node name*: if `sonos_den` was
ever selected as the default, `~/.local/state/wireplumber/default-nodes` keeps
it in the chain, and the moment the service creates that sink it becomes the
default again. Everything the user plays then goes to the speaker, and this
service faithfully makes the speaker play it.

An `inspect` mismatch hides it: `pactl get-default-sink` can report the onboard
device while the stored default is still the speaker. Check both.

```
wpctl inspect @DEFAULT_AUDIO_SINK@            # what new streams follow
grep '^default.configured.audio.sink' ~/.local/state/wireplumber/default-nodes
```

Fix it with the sink running, not stopped: `wpctl set-default <id of the real
sink>`. Setting it while the sink is absent looks fixed and is not, because the
in-memory chain rewrites the file on the next default-node event. Editing the
file by hand has the same problem.

## Failure symptoms

- No sinks: discovery found nothing, or pactl is not talking to pipewire-pulse.
- Sinks exist but the speaker is silent: ffmpeg could not open
  `sonos_<name>.monitor`, or the speaker is on a different subnet from 10.0.0.0/24
  and cannot reach the HTTP stream port.
- Speaker plays then stops after a few seconds: no client is playing into the
  sink, which is the auto-pause working as designed.
- No tray icon: `python3` does not resolve to `/usr/bin/python3`, or the item is
  still in the hidden-icons chevron.
