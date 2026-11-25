package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sonos-linux/audio"
	"sonos-linux/sonos"
	"sonos-linux/stream"
)

var requiredPackages = map[string]string{
	"pactl":  "pulseaudio-utils",
	"ffmpeg": "ffmpeg",
}

func ensureDeps() {
	var missing []string
	for bin, pkg := range requiredPackages {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return
	}

	log.Fatalf("Missing dependencies. Install with:\n  sudo apt install %s", strings.Join(missing, " "))
}

type deviceStream struct {
	device      *sonos.Device
	sinkName    string
	moduleIdx   int
	ffmpegCmd   *exec.Cmd
	player      *sonos.Player
	broadcaster *stream.Broadcaster
	streamURL   string
}

func main() {
	scanOnly := len(os.Args) > 1 && os.Args[1] == "--scan-devices"

	if scanOnly {
		runScan()
		return
	}

	ensureDeps()
	run()
}

func runScan() {
	fmt.Println("Discovering Sonos devices...")
	devices, err := sonos.Discover(5 * time.Second)
	if err != nil {
		log.Fatalf("Discovery failed: %v", err)
	}
	if len(devices) == 0 {
		log.Fatal("No Sonos devices found")
	}

	fmt.Printf("\nFound %d device(s):\n\n", len(devices))
	for _, d := range devices {
		printDevice(d)
	}
}

func printDevice(d *sonos.Device) {
	canPlay := "yes"
	if !d.CanPlay() {
		canPlay = "no"
	}
	coord := ""
	if d.IsCoord {
		coord = " (coordinator)"
	}
	fmt.Printf("  %s\n", d.Name)
	fmt.Printf("    IP:        %s\n", d.IP)
	fmt.Printf("    Model:     %s (%s)\n", d.ModelName, d.ModelNumber)
	fmt.Printf("    UUID:      %s\n", d.UUID)
	fmt.Printf("    Serial:    %s\n", d.SerialNumber)
	fmt.Printf("    Software:  %s\n", d.SoftwareVersion)
	fmt.Printf("    Hardware:  %s\n", d.HardwareVersion)
	fmt.Printf("    Group:     %s%s\n", d.GroupID, coord)
	fmt.Printf("    Invisible: %v\n", d.Invisible)
	fmt.Printf("    Can play:  %s\n", canPlay)
	fmt.Println()
}

func run() {
	// Discover devices
	fmt.Println("Discovering Sonos devices...")
	devices, err := sonos.Discover(5 * time.Second)
	if err != nil {
		log.Fatalf("Discovery failed: %v", err)
	}
	if len(devices) == 0 {
		log.Fatal("No Sonos devices found")
	}

	fmt.Printf("Found %d device(s)\n", len(devices))
	for _, d := range devices {
		printDevice(d)
	}

	// Filter to playable devices
	var playable []*sonos.Device
	for _, d := range devices {
		if d.CanPlay() {
			playable = append(playable, d)
		}
	}
	if len(playable) == 0 {
		log.Fatal("No playable Sonos devices found")
	}

	// Clean up orphaned sinks
	audio.CleanOrphanedSinks()

	// Determine local IP
	localIP, err := localIPFor(playable[0].IP)
	if err != nil {
		log.Fatalf("Failed to determine local IP: %v", err)
	}

	codec := audio.CodecFromEnv()
	fmt.Printf("Stream codec: %s (%s)\n", codec.Name, codec.ContentType)

	// Start HTTP server
	srv := stream.NewServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var streams []deviceStream

	// Set up a sink + encoder + stream for each playable device
	for _, dev := range playable {
		sanitized := audio.SanitizeName(dev.Name)
		sinkName := audio.SinkPrefix + sanitized
		description := "Sonos - " + dev.Name
		streamPath := "/stream/" + sanitized + "." + codec.Ext

		moduleIdx, err := audio.CreateSink(sinkName, description)
		if err != nil {
			log.Printf("Failed to create sink for %s: %v", dev.Name, err)
			continue
		}

		broadcaster := stream.NewBroadcaster(description, codec.ContentType)
		srv.AddStream(streamPath, broadcaster)

		ffmpegCmd, stdout, err := audio.StartEncoder(ctx, sinkName, codec)
		if err != nil {
			log.Printf("Failed to start encoder for %s: %v", dev.Name, err)
			audio.DestroySink(moduleIdx)
			continue
		}

		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					broadcaster.Write(buf[:n])
				}
				if err != nil {
					// The encoder ended. Normally that is a suspend, a
					// PipeWire restart or a dead sink, and the process is
					// past saving: the stream server, the sink and the
					// speaker's URI all have to be built again. Let systemd
					// do that instead of rebuilding in place, which is how
					// this drifts from the startup path.
					if ctx.Err() == nil {
						log.Printf("[%s] encoder stopped: %v, restarting", dev.Name, err)
						os.Exit(1)
					}
					return
				}
			}
		}()

		streams = append(streams, deviceStream{
			device:      dev,
			sinkName:    sinkName,
			moduleIdx:   moduleIdx,
			ffmpegCmd:   ffmpegCmd,
			player:      sonos.NewPlayer(dev),
			broadcaster: broadcaster,
		})

		fmt.Printf("  Sonos - %-20s  sink: %s\n", dev.Name, sinkName)
	}

	if len(streams) == 0 {
		log.Fatal("Failed to set up any device streams")
	}

	port, err := srv.Start()
	if err != nil {
		log.Fatalf("Failed to start stream server: %v", err)
	}

	time.Sleep(time.Second)

	// Tell each Sonos to play its stream
	for i := range streams {
		s := &streams[i]
		sanitized := audio.SanitizeName(s.device.Name)
		s.streamURL = fmt.Sprintf("http://%s:%d/stream/%s.%s",
			localIP, port, sanitized, codec.Ext)

		fmt.Printf("  %s → %s\n", s.device.Name, s.streamURL)

		s.player.Stop()
		attachAndPlay(s)
	}

	// Start volume sync, activity monitor and the liveness check for each stream
	for i := range streams {
		s := &streams[i]
		go syncVolume(ctx, *s)
		go monitorActivity(ctx, s)
		go supervise(ctx, s)
	}

	// Start system tray
	tray := startTray(devices, streams)

	fmt.Println("\nRoute your audio to any 'Sonos - <name>' sink in your sound settings.")
	fmt.Println("Press Ctrl+C or use tray Quit to stop.")

	// Wait for quit signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
	case <-tray.quit:
	}

	// Cleanup
	fmt.Println("\nStopping...")
	tray.stop()
	for _, s := range streams {
		s.player.Stop()
	}
	cancel()
	for _, s := range streams {
		s.ffmpegCmd.Wait()
		audio.DestroySink(s.moduleIdx)
	}
	srv.Stop()
}

// syncVolume syncs the PulseAudio sink volume to the Sonos device.
// On startup, reads Sonos volume and sets the sink to match.
// Then polls for user changes and forwards them to Sonos.
func syncVolume(ctx context.Context, s deviceStream) {
	// Initialize sink volume from current Sonos volume
	if vol, err := s.player.GetVolume(); err == nil {
		var sonosVol int
		fmt.Sscanf(vol, "%d", &sonosVol)
		sinkVol := sonosVol * 65536 / 100
		audio.SetSinkVolume(s.sinkName, sinkVol)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Start tracking from the current sink volume so we don't
	// send a volume change on the first tick
	lastVolume := audio.GetSinkVolume(s.sinkName)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sinkVol := audio.GetSinkVolume(s.sinkName)
			if sinkVol < 0 || sinkVol == lastVolume {
				continue
			}
			lastVolume = sinkVol

			// Map sink volume (0-65536) to Sonos volume (0-100)
			sonosVol := sinkVol * 100 / 65536
			sonosVol = min(sonosVol, 100)
			s.player.SetVolume(sonosVol)
		}
	}
}

// attachAndPlay points the speaker at our stream and starts it.
//
// Re-attaching is the part that matters. A paused Sonos closes its HTTP
// connection, so a bare Play resumes a stream that no longer exists and the
// speaker sits there stopped. With no client attached, the URI has to be set
// again first.
func attachAndPlay(s *deviceStream) {
	if s.broadcaster == nil || s.broadcaster.Clients() == 0 {
		if err := s.player.SetAVTransportURI(s.streamURL); err != nil {
			log.Printf("Failed to set URI on %s: %v", s.device.Name, err)
			return
		}
	}
	if err := s.player.Play(); err != nil {
		log.Printf("Failed to start playback on %s: %v", s.device.Name, err)
	}
}

// idleGrace is how long the sink may have no input before the speaker is
// paused. A player swapping tracks drops its stream and opens a new one, and
// pausing on the first quiet tick made the speaker stop between tracks.
func idleGrace() time.Duration {
	if v := os.Getenv("SONOS_IDLE_GRACE"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs >= 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 4 * time.Second
}

// monitorActivity watches for sink inputs. When no apps have played to our sink
// for the grace period, pause the speaker. When an app starts playing, resume.
func monitorActivity(ctx context.Context, s *deviceStream) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	grace := idleGrace()
	wasActive := true // assume active on startup since we just called Play
	var quietSince time.Time
	lastHeal := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if audio.HasSinkInputs(s.sinkName) {
				quietSince = time.Time{}
				if !wasActive {
					log.Printf("[%s] Audio detected, resuming Sonos", s.device.Name)
					attachAndPlay(s)
					wasActive = true
					lastHeal = time.Now()
				} else if time.Since(lastHeal) >= 10*time.Second {
					// The speaker can end up stopped or paused while audio
					// keeps flowing, which is what a dropped stream looks
					// like from here. Point it back at the stream instead of
					// waiting for an input transition that never comes.
					switch st := s.player.TransportState(); st {
					case "", "PLAYING", "TRANSITIONING":
					default:
						log.Printf("[%s] speaker is %s while audio plays, re-attaching",
							s.device.Name, st)
						attachAndPlay(s)
					}
					lastHeal = time.Now()
				}
				continue
			}
			if quietSince.IsZero() {
				quietSince = time.Now()
				continue
			}
			if wasActive && time.Since(quietSince) >= grace {
				log.Printf("[%s] Silent for %.0fs, pausing Sonos",
					s.device.Name, grace.Seconds())
				s.player.Pause()
				wasActive = false
			}
		}
	}
}

// supervise notices the states this process cannot recover from in place: the
// sink disappeared, which is what a suspend or a PipeWire restart looks like,
// or the speaker has gone away. Exiting lets systemd rebuild the sink, the
// encoder and the stream in the same order the startup path does.
func supervise(ctx context.Context, s *deviceStream) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !audio.SinkExists(s.sinkName) {
				log.Printf("[%s] sink %s disappeared, restarting", s.device.Name, s.sinkName)
				os.Exit(1)
			}
		}
	}
}

func localIPFor(remoteIP string) (string, error) {
	conn, err := net.Dial("udp", remoteIP+":1400")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
