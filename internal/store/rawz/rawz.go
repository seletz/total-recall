// Package rawz compresses the raw lines of events (events.raw, schema v5)
// with zstd and an embedded, trained dictionary. It is the one place that
// knows the on-disk form of raw: a zstd frame with the dictionary ID in its
// header and the uncompressed size as frame content size, without a
// checksum. The decoder knows every dictionary that was ever embedded and
// frames written without one, so a newer dictionary never makes older rows
// unreadable; the encoder uses the newest (Dicts, DictID).
//
// The dictionaries are trained only from synthetic lines (the demo
// archive): `mise run dict:train`, see tools/rawdict.
package rawz

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

//go:embed dict/raw-v1.zdict
var dictV1 []byte

//go:embed dict/raw-v2.zdict
var dictV2 []byte

// Dicts are all dictionaries ever embedded, oldest first. Encode uses the
// last one; Decode accepts all of them. Append a new dictionary, never
// remove or replace one that was released. v1 was trained on the extracts
// schema v5 stored, v2 on whole lines (#180).
var Dicts = [][]byte{dictV1, dictV2}

// Dictionary IDs ("TR" and a counter).
const (
	// DictIDV1 is the dictionary ID of the first dictionary.
	DictIDV1 uint32 = 0x54520001
	// DictIDV2 is the dictionary ID of the second dictionary, for whole lines.
	DictIDV2 uint32 = 0x54520002
)

// ErrNotZstd is returned by Decode for a blob that is not a zstd frame.
var ErrNotZstd = errors.New("rawz: not a zstd frame")

// Codec encodes and decodes raw lines with a fixed set of dictionaries. It
// is safe for concurrent use.
type Codec struct {
	enc *zstd.Encoder
	dec *zstd.Decoder
}

// New returns a Codec that encodes with the last of dicts and decodes with
// all of them. Without dicts it works without a dictionary.
func New(dicts ...[]byte) (*Codec, error) {
	eopts := []zstd.EOption{
		zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
		zstd.WithEncoderConcurrency(1),
		zstd.WithEncoderCRC(false),
		zstd.WithLowerEncoderMem(true),
		// Always write the content size (also below 1 KiB): Inspect reads it.
		zstd.WithSingleSegment(true),
		// Most rows are a few KiB; a 1 MiB window keeps the decoder's
		// memory small, and a line beyond it (a large Write) still
		// compresses, only without matches farther back than the window.
		zstd.WithWindowSize(1 << 20),
	}
	if len(dicts) > 0 {
		eopts = append(eopts, zstd.WithEncoderDict(dicts[len(dicts)-1]))
	}
	enc, err := zstd.NewWriter(nil, eopts...)
	if err != nil {
		return nil, fmt.Errorf("rawz: encoder: %w", err)
	}
	dopts := []zstd.DOption{zstd.WithDecoderConcurrency(0), zstd.WithDecoderLowmem(true)}
	if len(dicts) > 0 {
		dopts = append(dopts, zstd.WithDecoderDicts(dicts...))
	}
	dec, err := zstd.NewReader(nil, dopts...)
	if err != nil {
		return nil, fmt.Errorf("rawz: decoder: %w", err)
	}
	return &Codec{enc: enc, dec: dec}, nil
}

// Encode compresses s. An empty string gives nil (stored as NULL).
func (c *Codec) Encode(s string) []byte {
	if s == "" {
		return nil
	}
	return c.enc.EncodeAll([]byte(s), make([]byte, 0, len(s)/3+32))
}

// Decode decompresses b. nil or empty gives "". A blob that is not a zstd
// frame is ErrNotZstd; a frame with an unknown dictionary is an error too.
func (c *Codec) Decode(b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	if !IsFrame(b) {
		return "", ErrNotZstd
	}
	out, err := c.dec.DecodeAll(b, nil)
	if err != nil {
		return "", fmt.Errorf("rawz: decode: %w", err)
	}
	return string(out), nil
}

// zstd frame magic number, little endian.
var frameMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

// IsFrame reports whether b starts like a zstd frame.
func IsFrame(b []byte) bool {
	return len(b) >= 4 && b[0] == frameMagic[0] && b[1] == frameMagic[1] && b[2] == frameMagic[2] && b[3] == frameMagic[3]
}

// Header describes a stored frame without decompressing it.
type Header struct {
	// DictID is the dictionary the frame was written with, 0 for none.
	DictID uint32
	// Size is the uncompressed size; ok of Inspect is false when the frame
	// does not carry it.
	Size uint64
}

// Inspect reads dictionary ID and uncompressed size from the frame header
// of b. ok is false if b is not a frame with a known content size.
func Inspect(b []byte) (h Header, ok bool) {
	var zh zstd.Header
	if err := zh.Decode(b); err != nil || !zh.HasFCS {
		return Header{}, false
	}
	return Header{DictID: zh.DictionaryID, Size: zh.FrameContentSize}, true
}

var (
	defaultOnce  sync.Once
	defaultCodec *Codec
	defaultErr   error
)

// Default returns the Codec with the embedded dictionaries (Dicts).
func Default() (*Codec, error) {
	defaultOnce.Do(func() { defaultCodec, defaultErr = New(Dicts...) })
	return defaultCodec, defaultErr
}

// Encode compresses s with the embedded dictionary (Default). It panics
// only if the embedded dictionary is unusable, which the tests catch.
func Encode(s string) []byte {
	c, err := Default()
	if err != nil {
		panic(err)
	}
	return c.Encode(s)
}

// Decode decompresses a blob written by Encode or by an earlier dictionary.
func Decode(b []byte) (string, error) {
	c, err := Default()
	if err != nil {
		return "", err
	}
	return c.Decode(b)
}
