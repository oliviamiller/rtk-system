package stationi2c

import (
	"context"
	"errors"
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
	i2cBus                  *i2c.I2C

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

// Start reads correction data from the i2c address
func (s *i2cCorrectionSource) Start(ready chan<- bool) {
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
		var err error
		// change log level
		logger.ChangePackageLogLevel("i2c", logger.InfoLevel)

		buf := make([]byte, 1024)

		for err == nil {
			select {
			case <-s.cancelCtx.Done():
				return
			default:
			}

			// Open I2C handle every time
			s.i2cBus, err = i2c.NewI2C(s.addr, s.bus)
			s.err.Set(err)

			_, err = s.i2cBus.ReadBytes(buf)
			s.err.Set(err)
			if err != nil {
				s.logger.Errorf("can't read bytes from i2c buffer: %s", err)
				return
			}

			// close I2C handle
			err = s.i2cBus.Close()
			s.err.Set(err)
			if err != nil {
				s.logger.Debug("failed to close i2c handle: %s", err)
				return
			}

		}
	})
}

// Close shuts down the i2cCorrectionSource.
func (s *i2cCorrectionSource) Close(ctx context.Context) error {
	s.cancelFunc()

	s.activeBackgroundWorkers.Wait()

	if err := s.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
