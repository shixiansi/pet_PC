package tts

import (
	"encoding/binary"
	"errors"
)

// wavDuration parses a WAV byte slice and returns its playing time in seconds.
//
// This exists because winmm's PlaySound offers no completion signal: it starts a
// clip and forgets about it. To know when a sentence has finished - which the
// pipeline needs, both to space out sentences and to know when the queue has
// drained - the only portable answer is to read the clip's own header and wait
// out its stated length.
//
// The parser is deliberately minimal. It walks the RIFF chunks rather than
// assuming a fixed 44-byte header, because the server writes its own file and the
// canonical header is not guaranteed: there is often a LIST/INFO chunk (which
// Windows itself adds when it touches a file), and some encoders emit a fact
// chunk before the data.
//
// A malformed file returns an error rather than a zero duration: a silent zero
// would make the pipeline treat a real sentence as instantaneous and play the
// whole reply on top of itself.
func wavDuration(data []byte) (float64, error) {
	const (
		riffHeaderLen = 12
		chunkHeader   = 8
		formatPCM     = 1
		formatFloat   = 3
		formatExt     = 0xFFFE
	)
	if len(data) < riffHeaderLen {
		return 0, errors.New("tts: 音频太短，不是完整的 WAV")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, errors.New("tts: 不是 WAV 数据")
	}

	var (
		byteRate   uint32
		bitsPerSam uint16
		channels   uint16
		sampleRate uint32
		formatTag  uint16
		dataBytes  uint32
		sawFormat  bool
		sawData    bool
	)

	// Walk the chunk list. Each chunk is a 4-byte id, a 4-byte little-endian
	// size, then that many bytes, padded to an even boundary.
	for off := riffHeaderLen; off+chunkHeader <= len(data); {
		id := string(data[off : off+4])
		size := binary.LittleEndian.Uint32(data[off+4 : off+8])
		body := off + chunkHeader
		end := body + int(size)
		if end > len(data) {
			// A truncated final chunk. If the data chunk was already read, the
			// duration is still usable; otherwise this is a broken file.
			if sawData {
				break
			}
			return 0, errors.New("tts: WAV 数据被截断")
		}

		switch id {
		case "fmt ":
			if size < 16 {
				return 0, errors.New("tts: WAV fmt 块不完整")
			}
			formatTag = binary.LittleEndian.Uint16(data[body : body+2])
			channels = binary.LittleEndian.Uint16(data[body+2 : body+4])
			sampleRate = binary.LittleEndian.Uint32(data[body+4 : body+8])
			byteRate = binary.LittleEndian.Uint32(data[body+8 : body+12])
			bitsPerSam = binary.LittleEndian.Uint16(data[body+14 : body+16])
			sawFormat = true
		case "data":
			dataBytes = size
			sawData = true
		}

		// Chunks are word-aligned: an odd size is followed by a pad byte.
		if size%2 == 1 {
			end++
		}
		if end <= off {
			// Defensive: a zero-size loop would otherwise not terminate.
			break
		}
		off = end

		// The data chunk is the last thing that matters, and stopping here also
		// avoids tripping over whatever trailing metadata follows it.
		if sawData {
			break
		}
	}

	if !sawFormat {
		return 0, errors.New("tts: WAV 缺少 fmt 块")
	}
	if !sawData {
		return 0, errors.New("tts: WAV 缺少 data 块")
	}
	switch formatTag {
	case formatPCM, formatFloat, formatExt:
	default:
		return 0, errors.New("tts: 不支持的 WAV 编码")
	}
	if byteRate == 0 {
		// Some encoders leave this zero. It is derivable from the rest, so
		// recovering is strictly better than failing on an otherwise playable
		// file.
		if channels == 0 || sampleRate == 0 || bitsPerSam == 0 {
			return 0, errors.New("tts: WAV 格式信息不完整")
		}
		byteRate = sampleRate * uint32(channels) * uint32(bitsPerSam) / 8
	}
	if byteRate == 0 {
		return 0, errors.New("tts: WAV 字节率无效")
	}
	return float64(dataBytes) / float64(byteRate), nil
}

// WAVDuration is the exported form, for callers outside this package that need
// to know how long a clip will play.
func WAVDuration(data []byte) (float64, error) { return wavDuration(data) }
