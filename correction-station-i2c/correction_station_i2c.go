package stationi2c

import (
	"context"
	"sync"

	i2c "github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"
	"github.com/edaniels/golog"
	"github.com/pkg/errors"
	"go.viam.com/utils"

	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/resource"
)

var (
	Model               = resource.NewModel("viam-labs", "sensor", "correction-station-i2c")
	errRequiredAccuracy = errors.New("required accuracy can be a fixed number 1-5, 5 being the highest accuracy")
)

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: func(
				ctx context.Context,
				deps resource.Dependencies,
				conf resource.Config,
				logger golog.Logger,
			) (sensor.Sensor, error) {
				newConf, err := resource.NativeConfig[*Config](conf)
				if err != nil {
					return nil, err
				}
				return newRTKStationI2C(ctx, deps, conf.ResourceName(), newConf, logger)
			},
		})
}

// Config is used for the correction-station-i2c attributes
type Config struct {
	RequiredAccuracy float64 `json:"required_accuracy,omitempty"` // fixed number 1-5, 5 being the highest accuracy
	RequiredTime     int     `json:"required_time_sec,omitempty"`

	I2CBus      int `json:"i2c_bus"`
	I2CAddr     int `json:"i2c_addr"`
	I2CBaudRate int `json:"i2c_baud_rate,omitempty"`

	// TestChan is a fake i2c bus for testing use only
	TestChan chan []uint8 `json:"-"`
}

// Validate ensures all parts of the config are valid.
func (cfg *Config) Validate(path string) ([]string, error) {
	var deps []string
	if cfg.RequiredAccuracy == 0 {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "required_accuracy")
	}
	if cfg.RequiredAccuracy < 0 || cfg.RequiredAccuracy > 5 {
		return nil, errRequiredAccuracy
	}
	if cfg.RequiredTime == 0 {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "required_time")
	}

	if cfg.I2CBus == 0 {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "i2c_bus")
	}
	if cfg.I2CAddr == 0 {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "i2c_addr")
	}

	return deps, nil
}

type rtkStationI2C struct {
	resource.Named
	resource.AlwaysRebuild
	logger  golog.Logger
	i2cPath i2cBusAddr
	i2cBus  *i2c.I2C

	cancelCtx               context.Context
	cancelFunc              func()
	activeBackgroundWorkers sync.WaitGroup

	err movementsensor.LastError
}

type i2cBusAddr struct {
	bus  int
	addr byte
}

func newRTKStationI2C(
	ctx context.Context,
	deps resource.Dependencies,
	name resource.Name,
	newConf *Config,
	logger golog.Logger,
) (sensor.Sensor, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	r := &rtkStationI2C{
		Named:      name.AsNamed(),
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
		logger:     logger,
		err:        movementsensor.NewLastError(1, 1),
	}

	err := ConfigureBaseRTKStation(newConf)
	if err != nil {
		r.logger.Warn("rtk base station could not be configured")
	}

	// Init correction source
	r.i2cPath.addr = byte(newConf.I2CAddr)
	r.i2cPath.bus = newConf.I2CBus

	r.logger.Debug("Starting")

	r.start(ctx)
	return r, r.err.Get()
}

// Start starts reading from the correction source and sends corrections the i2c buffer.
func (r *rtkStationI2C) start(ctx context.Context) {
	r.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer r.activeBackgroundWorkers.Done()

		if err := r.cancelCtx.Err(); err != nil {
			return
		}
		select {
		case <-r.cancelCtx.Done():
			return
		default:
		}

		var err error
		// change log level
		logger.ChangePackageLogLevel("i2c", logger.InfoLevel)

		buf := make([]byte, 1024)

		for err == nil {
			select {
			case <-r.cancelCtx.Done():
				return
			default:
			}

			// Open I2C handle every time
			r.i2cBus, err = i2c.NewI2C(r.i2cPath.addr, r.i2cPath.bus)
			r.err.Set(err)

			// Read correction data
			_, err = r.i2cBus.ReadBytes(buf)
			r.err.Set(err)
			if err != nil {
				r.logger.Errorf("can't read bytes from i2c buffer: %s", err)
				return
			}

			// close I2C handle
			err = r.i2cBus.Close()
			r.err.Set(err)
			r.i2cBus = nil
			if err != nil {
				r.logger.Errorf("failed to close i2c handle: %s", err)
				return
			}
		}
	})
}

// Close shuts down the rtkStation.
func (r *rtkStationI2C) Close(ctx context.Context) error {
	r.cancelFunc()
	r.activeBackgroundWorkers.Wait()

	if r.i2cBus != nil {
		err := r.i2cBus.Close()
		r.err.Set(err)
		r.logger.Debug("failed to close i2c handle: %s", err)
	}

	if err := r.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (r *rtkStationI2C) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, errors.New("unimplemented")
}
