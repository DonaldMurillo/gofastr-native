package capture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// mp4Writer records H.264 and Opus samples into an MP4 whose index follows
// the media data. Devices without a platform muxer (the WebView adapter) ask
// for it in their start report. A file whose writer never closed has no index
// and does not play, like an interrupted platform recording.
type mp4Writer struct {
	mu     sync.Mutex
	file   *os.File
	offset int64 // where the next sample goes
	mdat   int64 // where the mdat header starts
	info   Info
	tracks [2]mp4Track
	err    error
	closed bool
}

type mp4Track struct {
	samples  []mp4Sample
	sps, pps []byte
	channels int
}

type mp4Sample struct {
	offset int64
	size   uint32
	clock  int64
	key    bool
}

const (
	videoTimescale = 90000
	audioTimescale = 48000
	movieTimescale = 1000
)

func newMP4Writer(path string, info Info) (*mp4Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	header := box("ftyp", []byte("isom"), u32(512), []byte("isomiso2avc1mp41"))
	// A 64-bit mdat header; its size is written at close.
	header = append(header, u32(1)...)
	header = append(header, "mdat"...)
	header = append(header, u64(0)...)
	if _, err := file.Write(header); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	w := &mp4Writer{file: file, offset: int64(len(header)), mdat: int64(len(header)) - 16, info: info}
	return w, nil
}

// write appends one sample. Video before the first keyframe is dropped, since
// a player cannot decode it.
func (w *mp4Writer) write(media Media) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.err != nil {
		return
	}
	var data []byte
	switch media.Track {
	case Video:
		track := &w.tracks[Video]
		data = w.avcc(track, media.Data)
		if len(data) == 0 || len(track.samples) == 0 && !media.Key || track.sps == nil || track.pps == nil {
			return
		}
	case Audio:
		if len(media.Data) == 0 {
			return
		}
		track := &w.tracks[Audio]
		if track.channels == 0 {
			// The TOC byte's stereo flag.
			track.channels = 1 + int(media.Data[0]>>2&1)
		}
		data = media.Data
	default:
		return
	}
	if _, err := w.file.Write(data); err != nil {
		w.err = err
		return
	}
	track := &w.tracks[media.Track]
	track.samples = append(track.samples, mp4Sample{offset: w.offset, size: uint32(len(data)), clock: media.Clock, key: media.Key})
	w.offset += int64(len(data))
}

// span returns the clock of the first and last video sample written.
func (w *mp4Writer) span() (start, end int64, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	samples := w.tracks[Video].samples
	if len(samples) == 0 {
		return 0, 0, false
	}
	return samples[0].clock, samples[len(samples)-1].clock, true
}

// avcc turns an Annex B access unit into length-prefixed NAL units. Parameter
// sets move to the sample description; access unit delimiters are dropped.
func (w *mp4Writer) avcc(track *mp4Track, annexB []byte) []byte {
	var out []byte
	for _, unit := range splitAnnexB(annexB) {
		switch unit[0] & 0x1f {
		case 7:
			if len(unit) >= 4 {
				track.sps = bytes.Clone(unit)
			}
		case 8:
			track.pps = bytes.Clone(unit)
		case 9:
		default:
			out = append(out, u32(uint32(len(unit)))...)
			out = append(out, unit...)
		}
	}
	return out
}

func splitAnnexB(data []byte) [][]byte {
	var units [][]byte
	start := -1
	for i := 0; i+2 < len(data); {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				units = append(units, trimZeros(data[start:i]))
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(data) {
		units = append(units, data[start:])
	}
	kept := units[:0]
	for _, unit := range units {
		if len(unit) > 0 {
			kept = append(kept, unit)
		}
	}
	return kept
}

// trimZeros drops the leading zero of a four-byte start code that follows.
func trimZeros(unit []byte) []byte {
	for len(unit) > 0 && unit[len(unit)-1] == 0 {
		unit = unit[:len(unit)-1]
	}
	return unit
}

// failed returns the error that stopped write, if one did.
func (w *mp4Writer) failed() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// close writes the index and closes the file. After a failed write it still
// tries, so the samples written before the failure play. Its error is about
// the index; failed reports the write.
func (w *mp4Writer) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("capture: the recording file is already closed")
	}
	w.closed = true
	return errors.Join(w.finish(), w.file.Close())
}

func (w *mp4Writer) finish() error {
	if _, err := w.file.WriteAt(u64(uint64(w.offset-w.mdat)), w.mdat+8); err != nil {
		return err
	}
	start := int64(-1)
	for _, track := range w.tracks {
		if len(track.samples) > 0 && (start < 0 || track.samples[0].clock < start) {
			start = track.samples[0].clock
		}
	}
	var traks [][]byte
	movieDuration := uint32(0)
	next := uint32(1)
	video := &w.tracks[Video]
	if len(video.samples) > 0 {
		trak, duration := w.trak(next, video, videoTimescale, start)
		traks = append(traks, trak)
		movieDuration = max(movieDuration, duration)
		next++
	}
	if audio := &w.tracks[Audio]; len(audio.samples) > 0 {
		trak, duration := w.trak(next, audio, audioTimescale, start)
		traks = append(traks, trak)
		movieDuration = max(movieDuration, duration)
		next++
	}
	mvhd := fullBox("mvhd", 0, 0, u32(0), u32(0), u32(movieTimescale), u32(movieDuration), u32(0x00010000), u16(0x0100), u16(0), u64(0), matrix(0, 0, 0), make([]byte, 24), u32(next))
	moov := box("moov", append([][]byte{mvhd}, traks...)...)
	// A failed write may have left part of a sample after the last whole one.
	if err := w.file.Truncate(w.offset); err != nil {
		return err
	}
	if _, err := w.file.Seek(w.offset, io.SeekStart); err != nil {
		return err
	}
	_, err := w.file.Write(moov)
	return err
}

// trak describes one track. Its edit list delays a track that started after
// the other, so audio and video stay in step.
func (w *mp4Writer) trak(id uint32, track *mp4Track, timescale int64, start int64) ([]byte, uint32) {
	isVideo := timescale == videoTimescale
	count := len(track.samples)
	durations := make([]uint32, count)
	ticks := func(clock int64) int64 { return (clock - track.samples[0].clock) * timescale / 1e9 }
	fallback := int64(timescale / 30)
	if !isVideo {
		fallback = timescale / 50
	}
	var total int64
	for i := range track.samples {
		var duration int64
		if i+1 < count {
			duration = ticks(track.samples[i+1].clock) - total
		} else if i > 0 {
			duration = int64(durations[i-1])
		} else {
			duration = fallback
		}
		duration = max(duration, 1)
		durations[i] = uint32(duration)
		total += duration
	}
	mediaDuration := total
	trackDuration := uint32(mediaDuration * movieTimescale / timescale)
	delay := uint32((track.samples[0].clock - start) / 1e6)

	var stts []byte
	entries := uint32(0)
	for i := 0; i < count; {
		j := i
		for j < count && durations[j] == durations[i] {
			j++
		}
		stts = append(stts, u32(uint32(j-i))...)
		stts = append(stts, u32(durations[i])...)
		entries++
		i = j
	}
	sizes, offsets := make([]byte, 0, 4*count), make([]byte, 0, 8*count)
	var sync []byte
	syncCount := uint32(0)
	for i, sample := range track.samples {
		sizes = append(sizes, u32(sample.size)...)
		offsets = append(offsets, u64(uint64(sample.offset))...)
		if sample.key {
			sync = append(sync, u32(uint32(i+1))...)
			syncCount++
		}
	}
	var entry, handler, header []byte
	var width, height, volume uint32
	if isVideo {
		// The track header keeps the coded size too; its matrix rotates it.
		width, height = uint32(w.info.Width), uint32(w.info.Height)
		avcC := box("avcC", []byte{1, track.sps[1], track.sps[2], track.sps[3], 0xff, 0xe1}, u16(uint16(len(track.sps))), track.sps, []byte{1}, u16(uint16(len(track.pps))), track.pps)
		entry = box("avc1", make([]byte, 6), u16(1), make([]byte, 16), u16(uint16(width)), u16(uint16(height)), u32(0x00480000), u32(0x00480000), u32(0), u16(1), make([]byte, 32), u16(0x18), u16(0xffff), avcC)
		handler = fullBox("hdlr", 0, 0, u32(0), []byte("vide"), make([]byte, 12), []byte("VideoHandler\x00"))
		header = fullBox("vmhd", 0, 1, make([]byte, 8))
	} else {
		channels := uint16(max(track.channels, 1))
		dOps := box("dOps", []byte{0, byte(channels)}, u16(0), u32(48000), u16(0), []byte{0})
		entry = box("Opus", make([]byte, 6), u16(1), make([]byte, 8), u16(channels), u16(16), u16(0), u16(0), u32(48000<<16), dOps)
		handler = fullBox("hdlr", 0, 0, u32(0), []byte("soun"), make([]byte, 12), []byte("SoundHandler\x00"))
		header = fullBox("smhd", 0, 0, make([]byte, 4))
		volume = 0x0100
	}
	tables := [][]byte{
		fullBox("stsd", 0, 0, u32(1), entry),
		fullBox("stts", 0, 0, u32(entries), stts),
	}
	if isVideo {
		tables = append(tables, fullBox("stss", 0, 0, u32(syncCount), sync))
	}
	tables = append(tables,
		fullBox("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
		fullBox("stsz", 0, 0, u32(0), u32(uint32(count)), sizes),
		fullBox("co64", 0, 0, u32(uint32(count)), offsets),
	)
	dinf := box("dinf", fullBox("dref", 0, 0, u32(1), fullBox("url ", 0, 1)))
	minf := box("minf", header, dinf, box("stbl", tables...))
	mdhd := fullBox("mdhd", 1, 0, u64(0), u64(0), u32(uint32(timescale)), u64(uint64(mediaDuration)), u16(0x55c4), u16(0))
	mdia := box("mdia", mdhd, handler, minf)

	rotation := 0
	if isVideo {
		rotation = w.info.Rotation
	}
	tkhd := fullBox("tkhd", 0, 3, u32(0), u32(0), u32(id), u32(0), u32(trackDuration+delay), u64(0), u16(0), u16(0), u16(uint16(volume)), u16(0),
		matrix(rotation, width, height), u32(width<<16), u32(height<<16))
	parts := [][]byte{tkhd}
	if delay > 0 {
		elst := fullBox("elst", 0, 0, u32(2),
			u32(delay), u32(0xffffffff), u16(1), u16(0),
			u32(trackDuration), u32(0), u16(1), u16(0))
		parts = append(parts, box("edts", elst))
	}
	parts = append(parts, mdia)
	return box("trak", parts...), trackDuration + delay
}

// matrix is a track's display matrix for a clockwise rotation, written the
// way ffmpeg and Android's muxer write it.
func matrix(rotation int, width, height uint32) []byte {
	const one, minus = 0x00010000, 0xffff0000
	a, b, c, d, x, y := uint32(one), uint32(0), uint32(0), uint32(one), uint32(0), uint32(0)
	switch rotation {
	case 90:
		a, b, c, d, x = 0, one, minus, 0, height<<16
	case 180:
		a, d, x, y = minus, minus, width<<16, height<<16
	case 270:
		a, b, c, d, y = 0, minus, one, 0, width<<16
	}
	var out []byte
	for _, value := range []uint32{a, b, 0, c, d, 0, x, y, 0x40000000} {
		out = append(out, u32(value)...)
	}
	return out
}

func box(kind string, parts ...[]byte) []byte {
	size := 8
	for _, part := range parts {
		size += len(part)
	}
	out := make([]byte, 0, size)
	out = append(out, u32(uint32(size))...)
	out = append(out, kind...)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func fullBox(kind string, version byte, flags uint32, parts ...[]byte) []byte {
	head := u32(uint32(version)<<24 | flags&0xffffff)
	return box(kind, append([][]byte{head}, parts...)...)
}

func u16(value uint16) []byte { return binary.BigEndian.AppendUint16(nil, value) }
func u32(value uint32) []byte { return binary.BigEndian.AppendUint32(nil, value) }
func u64(value uint64) []byte { return binary.BigEndian.AppendUint64(nil, value) }

// finishedMP4 reports whether an MP4 file has its index (moov), which a
// muxer writes when it finishes the file, and the movie duration the index
// gives. A file cut short by a crash has none and cannot play.
func finishedMP4(path string) (time.Duration, bool) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, false
	}
	start, end, ok := findBox(file, "moov", 0, info.Size())
	if !ok {
		return 0, false
	}
	at, mvhdEnd, ok := findBox(file, "mvhd", start, end)
	if !ok {
		return 0, true
	}
	header := make([]byte, 32)
	n, _ := file.ReadAt(header, at)
	header = header[:n]
	// Version 0 holds 32-bit times, version 1 64-bit ones.
	var timescale, duration uint64
	switch {
	case len(header) >= 20 && header[0] == 0 && at+20 <= mvhdEnd:
		timescale, duration = uint64(binary.BigEndian.Uint32(header[12:])), uint64(binary.BigEndian.Uint32(header[16:]))
	case len(header) >= 32 && header[0] == 1 && at+32 <= mvhdEnd:
		timescale, duration = uint64(binary.BigEndian.Uint32(header[20:])), binary.BigEndian.Uint64(header[24:])
	}
	if timescale == 0 || duration > uint64(1<<62)/uint64(time.Second) {
		return 0, true
	}
	return time.Duration(duration * uint64(time.Second) / timescale), true
}

// findBox finds the first box of kind among the boxes from offset to end and
// returns where its content starts and ends. A box whose size runs past end,
// as an unfinished mdat's does, stops the search.
func findBox(file *os.File, kind string, offset, end int64) (int64, int64, bool) {
	header := make([]byte, 16)
	for offset+8 <= end {
		if _, err := file.ReadAt(header[:8], offset); err != nil {
			return 0, 0, false
		}
		size, headerSize := int64(binary.BigEndian.Uint32(header)), int64(8)
		if size == 1 {
			if _, err := file.ReadAt(header[8:16], offset+8); err != nil {
				return 0, 0, false
			}
			large := binary.BigEndian.Uint64(header[8:16])
			if large > uint64(end-offset) {
				return 0, 0, false
			}
			size, headerSize = int64(large), 16
		} else if size == 0 {
			size = end - offset
		}
		if size < headerSize || size > end-offset {
			return 0, 0, false
		}
		if string(header[4:8]) == kind {
			return offset + headerSize, offset + size, true
		}
		offset += size
	}
	return 0, 0, false
}
