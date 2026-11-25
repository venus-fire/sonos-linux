package audio

import (
	"log"
	"os"
	"strings"
)

// Codec is the wire format used between the encoder, the HTTP server and the
// Sonos speaker. The speaker accepts mp3, wav and flac on http-get.
type Codec struct {
	// Name is the value accepted in SONOS_CODEC.
	Name string
	// Ext is the stream path suffix, e.g. ".flac".
	Ext string
	// ContentType is sent with the stream. Sonos sniffs the header and uses
	// it to pick a decoder.
	ContentType string
	// Args are the ffmpeg output options placed after "-i <monitor>".
	Args []string
}

// CodecFLAC streams lossless 48 kHz, which is the rate of the PipeWire
// monitor and therefore avoids a resample. About 900-1000 kbps of stereo.
var CodecFLAC = Codec{
	Name:        "flac",
	Ext:         "flac",
	ContentType: "audio/flac",
	Args: []string{
		"-acodec", "flac",
		"-compression_level", "5",
		"-ar", "48000",
		"-ac", "2",
		"-f", "flac",
	},
}

// CodecMP3 is the original upstream path: 192k CBR at 44.1 kHz with the bit
// reservoir disabled. Cheap on bandwidth, lossy, and it resamples 48 kHz down.
var CodecMP3 = Codec{
	Name:        "mp3",
	Ext:         "mp3",
	ContentType: "audio/mpeg",
	Args: []string{
		"-acodec", "libmp3lame",
		"-b:a", "192k",
		"-ar", "44100",
		"-ac", "2",
		"-reservoir", "0",
		"-f", "mp3",
	},
}

// CodecMP3320 is lossy but keeps the 48 kHz source rate, so it is the middle
// option between FLAC and the upstream 192k default.
var CodecMP3320 = Codec{
	Name:        "mp3-320",
	Ext:         "mp3",
	ContentType: "audio/mpeg",
	Args: []string{
		"-acodec", "libmp3lame",
		"-b:a", "320k",
		"-ar", "48000",
		"-ac", "2",
		"-f", "mp3",
	},
}

// CodecWAV is uncompressed 16-bit stereo at 48 kHz, 1536 kbps.
var CodecWAV = Codec{
	Name:        "wav",
	Ext:         "wav",
	ContentType: "audio/wav",
	Args: []string{
		"-acodec", "pcm_s16le",
		"-ar", "48000",
		"-ac", "2",
		"-f", "wav",
	},
}

// DefaultCodec is what runs when SONOS_CODEC is unset.
//
// FLAC was the default for a while because the speaker advertises it and the
// stream is lossless, but the Beam drops a long FLAC stream. Measured on this
// speaker: playback cycles PLAYING, then STOPPED every ten to thirty seconds,
// and only a re-attach revives it. MP3 at 320k played forty seconds straight
// with a track change in the middle and never dropped. MP3-320 also keeps the
// 48 kHz monitor rate, so it still avoids the resample the 192k path does.
var DefaultCodec = CodecMP3320

// CodecFromEnv resolves SONOS_CODEC. Anything unrecognised falls back to
// DefaultCodec and says so, because a silent fallback reads as a working
// setting.
func CodecFromEnv() Codec {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("SONOS_CODEC")))
	switch raw {
	case "", "mp3-320", "mp3_320", "mp3320":
		return CodecMP3320
	case "flac":
		return CodecFLAC
	case "mp3":
		return CodecMP3
	case "wav":
		return CodecWAV
	}
	log.Printf("Unknown SONOS_CODEC=%q, using %s", raw, DefaultCodec.Name)
	return DefaultCodec
}
