package audio

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// StartEncoder launches ffmpeg to capture audio from the named sink's monitor
// source and encode it as a continuous stream in the chosen codec.
func StartEncoder(ctx context.Context, sinkName string, codec Codec) (*exec.Cmd, io.ReadCloser, error) {
	args := []string{
		"-loglevel", "error",
		// Low-latency input
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
