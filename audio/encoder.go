package audio

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"
)

// encoderFirstBytes is how long a fresh encoder gets to produce its first byte
// before it is written off as stuck and restarted.
const encoderFirstBytes = 6 * time.Second

// prefixedCloser is a buffered reader that still closes the pipe underneath.
type prefixedCloser struct {
	*bufio.Reader
	closer io.Closer
}

func (p prefixedCloser) Close() error { return p.closer.Close() }

// StartEncoder launches ffmpeg to capture audio from the named sink's monitor
// source and encode it as a continuous stream in the chosen codec.
//
// A freshly created null sink can leave ffmpeg attached but producing nothing:
// the capture stream shows up in the mixer, the node reads running, and not one
// byte reaches the muxer. The speaker then sits stopped with a stream it cannot
// read. A second attempt against the now-warm monitor works, so the first bytes
// are awaited here and the process is restarted when they do not arrive.
func StartEncoder(ctx context.Context, sinkName string, codec Codec) (*exec.Cmd, io.ReadCloser, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		cmd, reader, err := startEncoderOnce(ctx, sinkName, codec)
		if err != nil {
			lastErr = err
			continue
		}
		buffered := bufio.NewReaderSize(reader, 32*1024)
		got := make(chan error, 1)
		go func() {
			_, err := buffered.Peek(1)
			got <- err
		}()
		select {
		case err := <-got:
			if err == nil {
				return cmd, prefixedCloser{buffered, reader}, nil
			}
			lastErr = fmt.Errorf("encoder for %s ended before producing audio: %w", sinkName, err)
		case <-time.After(encoderFirstBytes):
			lastErr = fmt.Errorf("no audio from %s.monitor within %s", sinkName, encoderFirstBytes)
		case <-ctx.Done():
			cmd.Process.Kill()
			return nil, nil, ctx.Err()
		}
		log.Printf("encoder attempt %d for %s produced nothing (%v), retrying",
			attempt, sinkName, lastErr)
		cmd.Process.Kill()
		cmd.Wait()
	}
	return nil, nil, lastErr
}

func startEncoderOnce(ctx context.Context, sinkName string, codec Codec) (*exec.Cmd, io.ReadCloser, error) {
	args := []string{
		"-loglevel", "error",
		// Low-latency input. The speaker buffers seconds regardless, so these
		// only matter for how quickly the first frame appears.
		"-fflags", "+nobuffer",
		"-flags", "+low_delay",
		"-probesize", "32",
		"-analyzeduration", "0",
		// PulseAudio capture
		"-f", "pulse",
		"-i", sinkName + ".monitor",
	}
	args = append(args, codec.Args...)
	args = append(args, "-flush_packets", "1", "pipe:1")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}
