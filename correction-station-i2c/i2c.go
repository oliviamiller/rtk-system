package stationi2c

import (
	"context"
	"errors"
	"io"
	"sync"

	i2c "github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"
	"github.com/edaniels/golog"
	"go.viam.com/utils"

	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/resource"
)

type i2cCorrectionSource struct {
	logger golog.Logger
	bus    int
	addr   byte

	cancelCtx               context.Context
	cancelFunc              func()
	activeBackgroundWorkers sync.WaitGroup

	correctionReaderMu sync.Mutex
	correctionReader   io.ReadCloser // reader for rctm corrections only

	err movementsensor.LastError
}

func newI2CCorrectionSource(
	deps resource.Dependencies,
	conf *Config,
	logger golog.Logger,
) (correctionSource, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	s := &i2cCorrectionSource{
		bus:        conf.I2CBus,
		addr:       byte(conf.I2CAddr),
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
		logger:     logger,
		// Overloaded boards can have flaky I2C busses. Only report errors if at least 5 of the
		// last 10 attempts have failed.
		err: movementsensor.NewLastError(10, 5),
	}

	return s, s.err.Get()
}

// Start reads correction data from the i2c address and sends it into the correctionReader.
func (s *i2cCorrectionSource) Start(ready chan<- bool) {
	s.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer s.activeBackgroundWorkers.Done()

		// currently not checking if rtcm message is valid, need to figure out how to integrate constant I2C byte message with rtcm3 scanner
		if err := s.cancelCtx.Err(); err != nil {
			return
		}

		s.correctionReaderMu.Lock()
		if err := s.cancelCtx.Err(); err != nil {
			s.correctionReaderMu.Unlock()
			return
		}
		s.correctionReaderMu.Unlock()
		select {
		case ready <- true:
		case <-s.cancelCtx.Done():
			return
		}
		// create i2c bus
		i2cBus, err := i2c.NewI2C(s.addr, s.bus)
		s.err.Set(err)

		// change so you don't see a million logs
		logger.ChangePackageLogLevel("i2c", logger.InfoLevel)

		buf := make([]byte, 1024)
		_, err = i2cBus.ReadBytes(buf)
		if err != nil {
			s.logger.Debug("Could not read from handle")
		}

		// close I2C handle
		err = i2cBus.Close()
		s.err.Set(err)
		if err != nil {
			s.logger.Debug("failed to close i2c handle: %s", err)
			return
		}

		for err == nil {
			select {
			case <-s.cancelCtx.Done():
				return
			default:
			}
			// Open I2C handle every time
			i2cBus, err := i2c.NewI2C(s.addr, s.bus)
			s.err.Set(err)

			_, err = i2cBus.ReadBytes(buf)
			s.err.Set(err)
			if err != nil {
				s.logger.Errorf("can't open gps i2c handle: %s", err)
				return
			}

			// close I2C handle
			err = i2cBus.Close()
			s.err.Set(err)
			if err != nil {
				s.logger.Debug("failed to close i2c handle: %s", err)
				return
			}

		}
	})
}

// Reader returns the i2cCorrectionSource's correctionReader if it exists.
func (s *i2cCorrectionSource) Reader() (io.ReadCloser, error) {
	if s.correctionReader == nil {
		return nil, errors.New("no stream")
	}

	return s.correctionReader, s.err.Get()
}

// Close shuts down the i2cCorrectionSource.
func (s *i2cCorrectionSource) Close(ctx context.Context) error {
	s.correctionReaderMu.Lock()
	s.cancelFunc()

	// close correction reader
	if s.correctionReader != nil {
		if err := s.correctionReader.Close(); err != nil {
			s.correctionReaderMu.Unlock()
			return err
		}
		s.correctionReader = nil
	}

	s.correctionReaderMu.Unlock()
	s.activeBackgroundWorkers.Wait()

	if err := s.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
