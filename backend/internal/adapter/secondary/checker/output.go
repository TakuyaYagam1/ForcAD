package checker

import (
	"io"
	"os"
	"sync"
)

type outputStream uint8

const (
	outputStdout outputStream = iota
	outputStderr
)

// checkerOutput owns the pipes: exec.Cmd receives files, so Cmd.Wait only
// waits for the process. A retained writer cannot keep process reaping blocked.
type checkerOutput struct {
	readers [2]*os.File
	writers [2]*os.File
	done    chan error
}

func newCheckerOutput() (*checkerOutput, error) {
	output := &checkerOutput{done: make(chan error, 2)}
	for i := range output.readers {
		reader, writer, err := os.Pipe()
		if err != nil {
			output.closeReaders()
			output.closeWriters()
			return nil, err
		}
		output.readers[i], output.writers[i] = reader, writer
	}
	return output, nil
}

func (output *checkerOutput) start(capture *outputCapture, limit chan<- struct{}) {
	streams := [...]outputStream{outputStdout, outputStderr}
	for i, reader := range output.readers {
		go func() {
			_, err := io.Copy(captureWriter{capture: capture, stream: streams[i], limit: limit}, reader)
			_ = reader.Close()
			output.done <- err
		}()
	}
}

func (output *checkerOutput) closeReaders() {
	for _, reader := range output.readers {
		if reader != nil {
			_ = reader.Close()
		}
	}
}

func (output *checkerOutput) closeWriters() {
	for _, writer := range output.writers {
		if writer != nil {
			_ = writer.Close()
		}
	}
}

type outputCapture struct {
	mu      sync.Mutex
	limit   int
	stdout  []byte
	stderr  []byte
	limited bool
}

func (capture *outputCapture) append(stream outputStream, output []byte) bool {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	remaining := capture.limit - len(capture.stdout) - len(capture.stderr)
	if remaining <= 0 {
		if len(output) != 0 {
			capture.limited = true
			return true
		}
		return false
	}
	if len(output) > remaining {
		output = output[:remaining]
		capture.limited = true
	}
	if stream == outputStdout {
		capture.stdout = append(capture.stdout, output...)
	} else {
		capture.stderr = append(capture.stderr, output...)
	}
	return capture.limited && len(output) == remaining
}

func (capture *outputCapture) snapshot() ([]byte, []byte) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]byte(nil), capture.stdout...), append([]byte(nil), capture.stderr...)
}

func (capture *outputCapture) wasLimited() bool {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.limited
}

type captureWriter struct {
	capture *outputCapture
	stream  outputStream
	limit   chan<- struct{}
}

func (writer captureWriter) Write(output []byte) (int, error) {
	if writer.capture.append(writer.stream, output) {
		select {
		case writer.limit <- struct{}{}:
		default:
		}
	}
	return len(output), nil
}

var _ io.Writer = captureWriter{}
