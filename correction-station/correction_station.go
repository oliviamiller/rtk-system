package rtkstation

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/edaniels/golog"
	"github.com/pkg/errors"
	"go.viam.com/utils"

	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/resource"
)

var stationModel = resource.NewModel("viam-labs", "sensor", "correction-station")

// StationConfig is used for converting RTK MovementSensor config attributes.
type StationConfig struct {
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

const (
	i2cStr    = "i2c"
	serialStr = "serial"
	ntripStr  = "ntrip"
	timeMode  = "time"
)

// ErrStationValidation contains the model substring for the available correction source types.
var (
	ErrStationValidation = fmt.Errorf("only serial, I2C are supported for %s", stationModel.Name)
	errRequiredAccuracy  = errors.New("required accuracy can be a fixed number 1-5, 5 being the highest accuracy")
)

// Validate ensures all parts of the config are valid.
func (cfg *StationConfig) Validate(path string) ([]string, error) {
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
		if cfg.I2CConfig.Board == "" {
			return nil, utils.NewConfigValidationFieldRequiredError(path, "board")
		}
		deps = append(deps, cfg.I2CConfig.Board)
		return deps, cfg.I2CConfig.ValidateI2C(path)
	case serialStr:
		if cfg.SerialConfig.SerialPath == "" {
			return nil, utils.NewConfigValidationFieldRequiredError(path, "serial_correction_path")
		}
	case "":
		return nil, utils.NewConfigValidationFieldRequiredError(path, "correction_source")
	default:
		return nil, ErrStationValidation
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

func init() {
	resource.RegisterComponent(
		sensor.API,
		stationModel,
		resource.Registration[sensor.Sensor, *StationConfig]{
			Constructor: newRTKStation,
		})

}

type rtkStation struct {
	resource.Named
	resource.AlwaysRebuild
	logger           golog.Logger
	correctionSource correctionSource
	protocol         string
	i2cPaths         []i2cBusAddr
	serialPorts      []io.Writer
	serialWriter     io.Writer

	cancelCtx               context.Context
	cancelFunc              func()
	activeBackgroundWorkers sync.WaitGroup

	err movementsensor.LastError
}

type correctionSource interface {
	Reader() (io.ReadCloser, error)
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
	conf resource.Config,
	logger golog.Logger,
) (sensor.Sensor, error) {
	newConf, err := resource.NativeConfig[*StationConfig](conf)
	if err != nil {
		return nil, err
	}

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	r := &rtkStation{
		Named:      conf.ResourceName().AsNamed(),
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
		logger:     logger,
		err:        movementsensor.NewLastError(1, 1),
	}

	r.protocol = newConf.Protocol

	// Init correction source
	switch r.protocol {
	case serialStr:
		r.correctionSource, err = newSerialCorrectionSource(newConf, logger)
		if err != nil {
			return nil, err
		}
	case i2cStr:
		r.correctionSource, err = newI2CCorrectionSource(deps, newConf, logger)
		if err != nil {
			return nil, err
		}
	default:
		// Invalid protocol
		return nil, fmt.Errorf("%s is not a valid correction source protocol", r.protocol)
	}

	err = ConfigureBaseRTKStation(conf)
	if err != nil {
		r.logger.Info("rtk base station could not be configured")
		return r, err
	}

	r.logger.Debug("Starting")

	r.Start(ctx)
	return r, r.err.Get()
}

// Start starts reading from the correction source and sends corrections to the child movementsensor's.
func (r *rtkStation) Start(ctx context.Context) {
	r.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer r.activeBackgroundWorkers.Done()

		if err := r.cancelCtx.Err(); err != nil {
			return
		}

		// read from correction source
		ready := make(chan bool)
		r.correctionSource.Start(ready)

		select {
		case <-ready:
		case <-r.cancelCtx.Done():
			return
		}
		stream, err := r.correctionSource.Reader()
		if err != nil {
			r.logger.Errorf("Unable to get reader: %s", err)
			r.err.Set(err)
			return
		}

		reader := io.TeeReader(stream, r.serialWriter)

		// write corrections to all open ports and i2c handles
		for {
			select {
			case <-r.cancelCtx.Done():
				return
			default:
			}

			buf := make([]byte, 1100)
			n, err := reader.Read(buf)
			r.logger.Debugf("Reading %d bytes", n)
			if err != nil {
				if err.Error() == "io: read/write on closed pipe" {
					r.logger.Debug("Pipe closed")
					return
				}
				r.logger.Errorf("Unable to read stream: %s", err)
				r.err.Set(err)
				return
			}

			// write buf to all i2c handles
			for _, busAddr := range r.i2cPaths {
				// open handle
				handle, err := busAddr.bus.OpenHandle(busAddr.addr)
				if err != nil {
					r.logger.Errorf("can't open movementsensor i2c handle: %s", err)
					r.err.Set(err)
					return
				}
				// write to i2c handle
				err = handle.Write(ctx, buf)
				if err != nil {
					r.logger.Errorf("i2c handle write failed %s", err)
					r.err.Set(err)
					return
				}
				// close i2c handle
				err = handle.Close()
				if err != nil {
					r.logger.Errorf("failed to close handle: %s", err)
					r.err.Set(err)
					return
				}
			}
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

	// close all ports in slice
	for _, port := range r.serialPorts {
		err := port.(io.ReadWriteCloser).Close()
		if err != nil {
			return err
		}
	}

	if err := r.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (r *rtkStation) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, errors.New("inp")
}
