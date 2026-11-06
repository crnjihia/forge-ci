// path: internal/logs/demux.go
package logs

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
)

// StreamType identifies whether the log frame is stdout or stderr.
type StreamType string

const (
	StreamStdout StreamType = "stdout"
	StreamStderr StreamType = "stderr"
)

// DemuxDockerStream reads a Docker multiplexed 8-byte framed stream (used when container TTY is disabled)
// and emits line-delimited log chunks to the onLine callback.
// If the stream does not start with a valid Docker header, it falls back to standard text line scanning.
func DemuxDockerStream(r io.Reader, onLine func(stream StreamType, line string)) error {
	header := make([]byte, 8)
	lineBufs := map[StreamType]*bytes.Buffer{
		StreamStdout: new(bytes.Buffer),
		StreamStderr: new(bytes.Buffer),
	}

	for {
		// Read 8-byte header
		_, err := io.ReadFull(r, header)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			// Flush any remaining partial lines in buffers
			flushBuffer(lineBufs[StreamStdout], StreamStdout, onLine)
			flushBuffer(lineBufs[StreamStderr], StreamStderr, onLine)
			return nil
		}
		if err != nil {
			return err
		}

		var stream StreamType
		switch header[0] {
		case 1:
			stream = StreamStdout
		case 2:
			stream = StreamStderr
		default:
			// Fallback: If not standard Docker frame (e.g. plain text stream), treat header as plain text
			buf := bytes.NewBuffer(header)
			scanner := bufio.NewScanner(io.MultiReader(buf, r))
			for scanner.Scan() {
				onLine(StreamStdout, scanner.Text())
			}
			return scanner.Err()
		}

		frameSize := binary.BigEndian.Uint32(header[4:8])
		if frameSize == 0 {
			continue
		}

		frameData := make([]byte, frameSize)
		if _, err := io.ReadFull(r, frameData); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}

		// Process frameData line by line
		buf := lineBufs[stream]
		for i := 0; i < len(frameData); i++ {
			b := frameData[i]
			if b == '\n' {
				onLine(stream, buf.String())
				buf.Reset()
			} else if b != '\r' {
				buf.WriteByte(b)
			}
		}
	}
}

func flushBuffer(buf *bytes.Buffer, stream StreamType, onLine func(StreamType, string)) {
	if buf.Len() > 0 {
		onLine(stream, buf.String())
		buf.Reset()
	}
}

// EncodeDockerFrame encodes a string line into an 8-byte Docker multiplexed frame.
// Useful for creating test streams matching Docker daemon output.
func EncodeDockerFrame(stream StreamType, data string) []byte {
	streamByte := byte(1)
	if stream == StreamStderr {
		streamByte = byte(2)
	}

	payload := []byte(data)
	size := uint32(len(payload))

	frame := make([]byte, 8+len(payload))
	frame[0] = streamByte
	binary.BigEndian.PutUint32(frame[4:8], size)
	copy(frame[8:], payload)

	return frame
}

