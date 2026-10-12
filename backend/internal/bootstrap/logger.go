package bootstrap

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wahrwelt-kit/go-logkit"
)

const (
	logBufferCapacity = 4096
	logCloseTimeout   = 2 * time.Second
)

type Logger struct {
	*slog.Logger

	dropped   atomic.Int64
	output    *logWriter
	library   logkit.Logger
	closeOnce sync.Once
	closeErr  error
}

func NewLogger(output io.Writer) (*Logger, error) {
	const location = "bootstrap.NewLogger"
	if output == nil {
		return nil, fmt.Errorf("%s - output must be set", location)
	}
	logger := &Logger{}
	logger.output = &logWriter{sink: output, queue: make(chan []byte, logBufferCapacity), done: make(chan struct{}), dropped: &logger.dropped}
	library, err := logkit.New(
		logkit.WithWriter(logger.output),
		logkit.WithLevel(logkit.InfoLevel),
		logkit.WithServiceName("forcad"),
	)
	if err != nil {
		return nil, fmt.Errorf("%s - create logkit logger: %w", location, err)
	}
	logger.library = library
	logger.Logger = slog.New(logkit.SlogHandler(library))
	go logger.output.run()
	return logger, nil
}

// Close stops logkit before draining the queue. The result is cached, so a drain timeout remains the result of later calls.
func (logger *Logger) Close() error {
	const location = "bootstrap.Logger.Close"
	logger.closeOnce.Do(func() {
		if err := logger.library.Close(); err != nil {
			logger.closeErr = fmt.Errorf("%s - close logkit logger: %w", location, err)
		}
		if err := logger.output.close(); err != nil {
			logger.closeErr = errors.Join(logger.closeErr, fmt.Errorf("%s - drain log output: %w", location, err))
		}
	})
	return logger.closeErr
}

// Container output can stall; requests must not wait for the log sink.
type logWriter struct {
	sink    io.Writer
	queue   chan []byte
	done    chan struct{}
	dropped *atomic.Int64
	mu      sync.RWMutex
	closed  bool
	err     error
}

func (writer *logWriter) Write(data []byte) (int, error) {
	const location = "bootstrap.logWriter.Write"
	writer.mu.RLock()
	defer writer.mu.RUnlock()
	if writer.closed {
		return 0, fmt.Errorf("%s - logger is closed: %w", location, io.ErrClosedPipe)
	}
	select {
	case writer.queue <- append([]byte(nil), data...):
	default:
		writer.dropped.Add(1)
	}
	return len(data), nil
}

func (writer *logWriter) run() {
	defer close(writer.done)
	for data := range writer.queue {
		n, err := writer.sink.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		if err != nil {
			writer.dropped.Add(1)
			if writer.err == nil {
				writer.err = err
			}
		}
	}
}

func (writer *logWriter) close() error {
	const location = "bootstrap.logWriter.close"
	writer.mu.Lock()
	if !writer.closed {
		writer.closed = true
		close(writer.queue)
	}
	writer.mu.Unlock()
	timer := time.NewTimer(logCloseTimeout)
	defer timer.Stop()
	select {
	case <-writer.done:
		if writer.err != nil {
			return fmt.Errorf("%s - write log output: %w", location, writer.err)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("%s - log output did not drain within %s", location, logCloseTimeout)
	}
}
