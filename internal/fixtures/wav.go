// Package fixtures builds byte-level test fixtures shared across packages.
package fixtures

import (
	"encoding/binary"

	"github.com/DonaldMurillo/gofastr-native/device/files"
)

// WAV returns a minimal single-channel 16-bit PCM recording.wav document with
// the given sample rate and payload length in bytes. The header is 44 bytes,
// so len(document.Data) == 44+payload.
func WAV(sampleRate, payload int) files.Document {
	data := make([]byte, 44+payload)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(data[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(payload))
	return files.Document{Name: "recording.wav", MediaType: "audio/wav", Data: data}
}
