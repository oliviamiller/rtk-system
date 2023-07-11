package stationserial

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/edaniels/golog"
	"github.com/jacobsa/go-serial/serial"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

const (
	serialStr = "serial"
	timeMode  = "time"
)

var (
	Model               = resource.NewModel("viam-labs", "sensor", "correction-station_serial")
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
				return newSerialRTKStation(ctx, deps, conf.ResourceName(), newConf, logger)
			},
		})
}

type Config struct {
	RequiredAccuracy float64 `json:"required_accuracy,omitempty"` // fixed number 1-5, 5 being the highest accuracy
	RequiredTime     int     `json:"required_time_sec,omitempty"`

	SerialPath     string `json:"serial_path"`
	SerialBaudRate int    `json:"serial_baud_rate,omitempty"`

	// TestChan is a fake "serial" path for test use only
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
	if cfg.SerialPath == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "serial_path")
	}

	return deps, nil
}

type rtkStationSerial struct {
	resource.Named
	resource.AlwaysRebuild
	logger           golog.Logger
	correctionSource correctionSource
	protocol         string
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

func newSerialRTKStation(
	ctx context.Context,
	deps resource.Dependencies,
	name resource.Name,
	newConf *Config,
	logger golog.Logger,
) (sensor.Sensor, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	r := &rtkStationSerial{
		Named:      name.AsNamed(),
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
		logger:     logger,
		err:        movementsensor.NewLastError(1, 1),
	}

	err := ConfigureBaseRTKStation(newConf)
	if err != nil {
		r.logger.Info("rtk base station could not be configured")
		return r, err
	}

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

	r.logger.Debug("Starting")

	r.start(ctx)
	return r, r.err.Get()
}

// Start starts reading from the correction source and sends corrections to the radio/bluetooth.
func (r *rtkStationSerial) start(ctx context.Context) {
	r.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer r.activeBackgroundWorkers.Done()

		if err := r.cancelCtx.Err(); err != nil {
			return
		}

		// start the correction source
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
func (r *rtkStationSerial) Close(ctx context.Context) error {
	r.cancelFunc()
	r.activeBackgroundWorkers.Wait()

	// close correction source
	err := r.correctionSource.Close(ctx)
	if err != nil {
		return err
	}

	// close the serial port
	err = r.serialWriter.(io.ReadWriteCloser).Close()
	if err != nil {
		return err
	}

	if err := r.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// TODO: add readings for fix and num sats in view
func (r *rtkStationSerial) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, errors.New("unimplemented")
}
