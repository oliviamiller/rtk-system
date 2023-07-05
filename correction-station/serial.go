package station

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/edaniels/golog"
	"github.com/go-gnss/rtcm/rtcm3"
	"github.com/jacobsa/go-serial/serial"

	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/utils"
)

type serialCorrectionSource struct {
	port   io.ReadCloser // reads all messages from port
	logger golog.Logger

	cancelCtx               context.Context
	cancelFunc              func()
	activeBackgroundWorkers sync.WaitGroup

	err movementsensor.LastError

	// TestChan is a fake "serial" path for test use only
	TestChan chan []uint8 `json:"-"`
}

type pipeReader struct {
	pr *io.PipeReader
}

func (r pipeReader) Read(p []byte) (int, error) {
	return r.pr.Read(p)
}

func (r pipeReader) Close() error {
	return r.pr.Close()
}

type pipeWriter struct {
	pw *io.PipeWriter
}

func (r pipeWriter) Write(p []byte) (int, error) {
	return r.pw.Write(p)
}

func (r pipeWriter) Close() error {
	return r.pw.Close()
}

const (
	correctionPathName = "correction_path"
	baudRateName       = "correction_baud"
)

func newSerialCorrectionSource(conf *Config, logger golog.Logger) (correctionSource, error) {
	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	s := &serialCorrectionSource{
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
		logger:     logger,
		err:        movementsensor.NewLastError(1, 1),
	}

	serialPath := conf.SerialPath
	if serialPath == "" {
		return nil, fmt.Errorf("serial path expected non-empty string for %q", correctionPathName)
	}

	baudRate := conf.SerialBaudRate
	if baudRate == 0 {
		baudRate = 38400
		s.logger.Info("Using default baud rate 38400")
	}

	if conf.SerialConfig.TestChan != nil {
		s.TestChan = conf.SerialConfig.TestChan
	} else {
		options := serial.OpenOptions{
			PortName:        serialPath,
			BaudRate:        uint(baudRate),
			DataBits:        8,
			StopBits:        1,
			MinimumReadSize: 4,
		}

		var err error
		s.port, err = serial.Open(options)
		if err != nil {
			return nil, err
		}
	}

	return s, s.err.Get()
}

// Start reads correction data from the serial port and sends it into the correctionReader.
func (s *serialCorrectionSource) Start(ready chan<- bool) {
	s.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer s.activeBackgroundWorkers.Done()

		if err := s.cancelCtx.Err(); err != nil {
			return
		}

		select {
		case ready <- true:
		case <-s.cancelCtx.Done():
			return
		}

		// Read the rctm messages just to make sure that they are coming in, return if not.
		scanner := rtcm3.NewScanner(s.port)

		for {
			select {
			case <-s.cancelCtx.Done():
				return
			default:
			}
			msg, err := scanner.NextMessage()
			if err != nil {
				s.logger.Errorf("Error reading RTCM message: %s", err)
				s.err.Set(err)
				return
			}
			switch msg.(type) {
			case rtcm3.MessageUnknown:
				continue
			default:
			}
		}
	})
}

// Close shuts down the serialCorrectionSource and closes s.port.
func (s *serialCorrectionSource) Close(ctx context.Context) error {
	s.cancelFunc()

	// close port reader
	if s.port != nil {
		if err := s.port.Close(); err != nil {
			return err
		}
		s.port = nil
	}

	s.activeBackgroundWorkers.Wait()

	return s.err.Get()
}
