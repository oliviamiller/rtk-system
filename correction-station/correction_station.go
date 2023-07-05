package station

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/edaniels/golog"
	"github.com/jacobsa/go-serial/serial"
	"github.com/pkg/errors"
	"go.viam.com/utils"

	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/resource"
)

const (
	i2cStr    = "i2c"
	serialStr = "serial"
	timeMode  = "time"
)

var (
	StationModel         = resource.NewModel("viam-labs", "sensor", "correction-station")
	errStationValidation = fmt.Errorf("only serial, i2c are supported for %s", StationModel.Name)
	errRequiredAccuracy  = errors.New("required accuracy can be a fixed number 1-5, 5 being the highest accuracy")
)

func init() {
	resource.RegisterComponent(
		sensor.API,
		StationModel,
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
				return newRTKStation(ctx, deps, conf.ResourceName(), newConf, logger)
			},
		})
}

// Config is used for the base station attributes.
type Config struct {
	Protocol string `json:"protocol"`

	RequiredAccuracy float64 `json:"required_accuracy,omitempty"` // fixed number 1-5, 5 being the highest accuracy
	RequiredTime     int     `json:"required_time_sec,omitempty"`

	*SerialConfig `json:"serial_attributes,omitempty"`
	*I2CConfig    `json:"i2c_attributes,omitempty"`
}

// SerialConfig is used for converting attributes for a correction source.
type SerialConfig struct {
	SerialPath     string `json:"serial_path"`
	SerialBaudRate int    `json:"serial_baud_rate,omitempty"`

	// TestChan is a fake "serial" path for test use only
	TestChan chan []uint8 `json:"-"`
}

// I2CConfig is used for converting attributes for a correction source.
type I2CConfig struct {
	Board       string `json:"board"`
	I2CBus      string `json:"i2c_bus"`
	I2cAddr     int    `json:"i2c_addr"`
	I2CBaudRate int    `json:"i2c_baud_rate,omitempty"`
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

	switch cfg.Protocol {
	case i2cStr:
		return deps, cfg.I2CConfig.ValidateI2C(path)
	case serialStr:
		if cfg.SerialConfig.SerialPath == "" {
			return nil, utils.NewConfigValidationFieldRequiredError(path, "serial_path")
		}
	case "":
		return nil, utils.NewConfigValidationFieldRequiredError(path, "protocol")
	default:
		return nil, errStationValidation
	}

	return deps, nil
}

// ValidateI2C ensures all parts of the config are valid.
func (cfg *I2CConfig) ValidateI2C(path string) error {
	if cfg.I2CBus == "" {
		return utils.NewConfigValidationFieldRequiredError(path, "i2c_bus")
	}
	if cfg.I2cAddr == 0 {
		return utils.NewConfigValidationFieldRequiredError(path, "i2c_addr")
	}

	return nil
}

// ValidateSerial ensures all parts of the config are valid.
func (cfg *SerialConfig) ValidateSerial(path string) error {
	if cfg.SerialPath == "" {
		return utils.NewConfigValidationFieldRequiredError(path, "serial_path")
	}
	return nil
}

type rtkStation struct {
	resource.Named
	resource.AlwaysRebuild
	logger           golog.Logger
	correctionSource correctionSource
	protocol         string
	i2cPath          i2cBusAddr
	serialWriter     io.Writer

	cancelCtx               context.Context
	cancelFunc              func()
	activeBackgroundWorkers sync.WaitGroup

	err movementsensor.LastError
}

type correctionSource interface {
	Start(ready chan<- bool)
	Close(ctx context.Context) error
}

type i2cBusAddr struct {
	bus  board.I2C
	addr byte
}

func newRTKStation(
	ctx context.Context,
	deps resource.Dependencies,
	name resource.Name,
	newConf *Config,
	logger golog.Logger,
) (sensor.Sensor, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	r := &rtkStation{
		Named:      name.AsNamed(),
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
		logger:     logger,
		err:        movementsensor.NewLastError(1, 1),
	}

	r.protocol = newConf.Protocol

	err := ConfigureBaseRTKStation(newConf)
	if err != nil {
		r.logger.Error("rtk base station could not be configured")
		return r, err
	}

	// Init correction source
	switch r.protocol {
	case serialStr:
		r.correctionSource, err = newSerialCorrectionSource(newConf, logger)
		if err != nil {
			return nil, err
		}
		// set a default baud rate if not specficed in config
		if newConf.SerialBaudRate == 0 {
			newConf.SerialBaudRate = 38400
		}

		options := serial.OpenOptions{
			PortName:        newConf.SerialPath,
			BaudRate:        uint(newConf.SerialBaudRate),
			DataBits:        8,
			StopBits:        1,
			MinimumReadSize: 4,
		}

		port, err := serial.Open(options)
		if err != nil {
			return nil, err
		}

		r.logger.Debug("Init serial writer")
		r.serialWriter = io.Writer(port)
	case i2cStr:
		//TODO RSDK-3755 add i2c to this
	default:
		// Invalid protocol
		return nil, fmt.Errorf("%s is not a valid correction source protocol", r.protocol)
	}

	r.logger.Debug("Starting")

	r.start(ctx)
	return r, r.err.Get()
}

// Start starts reading from the correction source and sends corrections to the radio/bluetooth.
func (r *rtkStation) start(ctx context.Context) {
	r.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer r.activeBackgroundWorkers.Done()

		if err := r.cancelCtx.Err(); err != nil {
			return
		}

		// Start the correction source
		ready := make(chan bool)
		r.correctionSource.Start(ready)
		select {
		case <-ready:
		case <-r.cancelCtx.Done():
			return
		}
	})
}

// Close shuts down the rtkStation.
func (r *rtkStation) Close(ctx context.Context) error {
	r.cancelFunc()
	r.activeBackgroundWorkers.Wait()

	// close correction source
	err := r.correctionSource.Close(ctx)
	if err != nil {
		return err
	}

	if r.protocol == serialStr {
		// close the serial port
		err = r.serialWriter.(io.ReadWriteCloser).Close()
		if err != nil {
			return err
		}
	}

	if err := r.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// TODO: add readings for fix and num sats in view
func (r *rtkStation) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, errors.New("unimplemented")
}