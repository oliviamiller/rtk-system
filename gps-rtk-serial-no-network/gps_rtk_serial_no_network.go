package gpsrtkserialnonetwork

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	nmea "rtksystem/gps-nmea"
	"sync"

	"github.com/edaniels/golog"
	"github.com/go-gnss/rtcm/rtcm3"
	"github.com/golang/geo/r3"
	slib "github.com/jacobsa/go-serial/serial"
	geo "github.com/kellydunn/golang-geo"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
	"go.viam.com/utils"
)

var Model = resource.NewModel("viam-labs", "movement-sensor", "gps-rtk-serial-no-network")

const serialStr = "serial"

type Config struct {
	SerialNMEAPath           string `json:"serial_NMEA_path"`
	SerialNMEABaudRate       int    `json:"serial__NMEA_baud_rate,omitempty"`
	SerialCorrectionPath     string `json:"serial_correction_path"`
	SerialCorrectionBaudRate int    `json:"serial_correction_baud_rate"`
}

// ValidateSerial ensures all parts of the config are valid.
func (cfg *Config) Validate(path string) ([]string, error) {
	var deps []string
	if cfg.SerialNMEAPath == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "serial_NMEA_path")
	}
	if cfg.SerialCorrectionPath == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "serial_correction_path")
	}
	return deps, nil
}

func init() {
	resource.RegisterComponent(
		movementsensor.API,
		Model,
		resource.Registration[movementsensor.MovementSensor, *Config]{
			Constructor: func(
				ctx context.Context,
				deps resource.Dependencies,
				conf resource.Config,
				logger golog.Logger,
			) (movementsensor.MovementSensor, error) {
				newConf, err := resource.NativeConfig[*Config](conf)
				if err != nil {
					return nil, err
				}
				return newrtkSerialNoNetwork(ctx, deps, conf.ResourceName(), newConf, logger)
			},
		})
}

// A rtkSerialNoNetwork is a MovementSensor model that can intake RTK correction data from a serial path.
type rtkSerialNoNetwork struct {
	resource.Named
	resource.AlwaysRebuild
	logger     golog.Logger
	cancelCtx  context.Context
	cancelFunc func()

	activeBackgroundWorkers sync.WaitGroup

	err          movementsensor.LastError
	lastposition movementsensor.LastPosition

	nmeamovementsensor nmea.NmeaMovementSensor
	correctionWriter   io.ReadWriteCloser
	correctionReader   io.ReadCloser
	correctionReaderMu sync.Mutex

	writePath     string
	writeBaudRate int

	readPath     string
	readBaudRate int
}

func newrtkSerialNoNetwork(
	ctx context.Context,
	deps resource.Dependencies,
	name resource.Name,
	newConf *Config,
	logger golog.Logger,
) (movementsensor.MovementSensor, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	g := &rtkSerialNoNetwork{
		Named:        name.AsNamed(),
		cancelCtx:    cancelCtx,
		cancelFunc:   cancelFunc,
		logger:       logger,
		err:          movementsensor.NewLastError(1, 1),
		lastposition: movementsensor.NewLastPosition(),
	}

	nmeaConf := &nmea.Config{
		ConnectionType: serialStr,
	}

	// Init NMEAMovementSensor
	nmeaConf.SerialConfig = &nmea.SerialConfig{SerialPath: newConf.SerialNMEAPath, SerialBaudRate: newConf.SerialNMEABaudRate}
	var err error
	g.nmeamovementsensor, err = nmea.NewSerialGPSNMEA(ctx, name, nmeaConf, logger)
	if err != nil {
		return nil, err
	}

	g.writePath = newConf.SerialNMEAPath
	g.writeBaudRate = newConf.SerialNMEABaudRate

	if g.writeBaudRate == 0 {
		g.writeBaudRate = 38400
	}

	g.readPath = newConf.SerialCorrectionPath
	g.readBaudRate = newConf.SerialCorrectionBaudRate

	if g.writeBaudRate == 0 {
		g.writeBaudRate = 38400
	}

	if err := g.start(); err != nil {
		return nil, err
	}
	return g, g.err.Get()

}

// Start begins reading the nmea data and correction source readings
func (g *rtkSerialNoNetwork) start() error {
	if err := g.nmeamovementsensor.Start(g.cancelCtx); err != nil {
		g.lastposition.GetLastPosition()
		return err
	}
	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(g.receiveAndWriteSerial)

	return g.err.Get()
}

func (g *rtkSerialNoNetwork) getCorrectionReader() io.ReadCloser {

	if err := g.cancelCtx.Err(); err != nil {
		return nil
	}

	g.correctionReaderMu.Lock()
	defer g.correctionReaderMu.Unlock()

	options := slib.OpenOptions{
		PortName:        g.readPath,
		BaudRate:        uint(g.readBaudRate),
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 1,
	}

	var err error
	g.correctionReader, err = slib.Open(options)
	if err != nil {
		g.logger.Errorf("serial.Open: %v", err)
		g.err.Set(err)
		return nil
	}

	return g.correctionReader

}

// Recieves correction data from the base station serial port and writes to the gpsrtk
func (g *rtkSerialNoNetwork) receiveAndWriteSerial() {
	defer g.activeBackgroundWorkers.Done()
	if err := g.cancelCtx.Err(); err != nil {
		return
	}

	reader := g.getCorrectionReader()

	options := slib.OpenOptions{
		PortName:        g.writePath,
		BaudRate:        uint(g.writeBaudRate),
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 1,
	}

	// Open the port.
	if err := g.cancelCtx.Err(); err != nil {
		return
	}
	var err error
	g.correctionWriter, err = slib.Open(options)
	if err != nil {
		g.logger.Errorf("serial.Open: %v", err)
		g.err.Set(err)
		return
	}

	writer := bufio.NewWriter(g.correctionWriter)
	scanner := rtcm3.NewScanner(reader)

	for {
		select {
		case <-g.cancelCtx.Done():
			return
		default:
		}

		msg, err := scanner.NextMessage()

		switch msg.(type) {
		case rtcm3.MessageUnknown:
			continue
		default:
			frame := rtcm3.EncapsulateMessage(msg)
			byteMsg := frame.Serialize()
			writer.Write(byteMsg)
			if err != nil {
				g.logger.Errorf("Error writing RTCM message: %s", err)
				g.err.Set(err)
				return
			}
		}
		if err != nil {
			if msg == nil {
				g.logger.Debug("No message... reconnecting to stream...")
				scanner = rtcm3.NewScanner(reader)
				continue
			}
		}
	}

}

// Position returns the current geographic location of the MOVEMENTSENSOR.
func (g *rtkSerialNoNetwork) Position(ctx context.Context, extra map[string]interface{}) (*geo.Point, float64, error) {
	lastError := g.err.Get()
	if lastError != nil {
		lastPosition := g.lastposition.GetLastPosition()
		if lastPosition != nil {
			return lastPosition, 0, nil
		}
		return geo.NewPoint(math.NaN(), math.NaN()), math.NaN(), lastError
	}

	position, alt, err := g.nmeamovementsensor.Position(ctx, extra)
	if err != nil {
		// Use the last known valid position if current position is (0,0)/ NaN.
		if position != nil && (g.lastposition.IsZeroPosition(position) || g.lastposition.IsPositionNaN(position)) {
			lastPosition := g.lastposition.GetLastPosition()
			if lastPosition != nil {
				return lastPosition, alt, nil
			}
		}
		return geo.NewPoint(math.NaN(), math.NaN()), math.NaN(), err
	}

	// Check if the current position is different from the last position and non-zero
	lastPosition := g.lastposition.GetLastPosition()
	if !g.lastposition.ArePointsEqual(position, lastPosition) {
		g.lastposition.SetLastPosition(position)
	}

	// Update the last known valid position if the current position is non-zero
	if position != nil && !g.lastposition.IsZeroPosition(position) {
		g.lastposition.SetLastPosition(position)
	}

	return position, alt, nil
}

// LinearVelocity passthrough.
func (g *rtkSerialNoNetwork) LinearVelocity(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return r3.Vector{}, lastError
	}

	return g.nmeamovementsensor.LinearVelocity(ctx, extra)
}

// LinearAcceleration passthrough.
func (g *rtkSerialNoNetwork) LinearAcceleration(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return r3.Vector{}, lastError
	}
	return r3.Vector{}, nil
}

// AngularVelocity passthrough.
func (g *rtkSerialNoNetwork) AngularVelocity(ctx context.Context, extra map[string]interface{}) (spatialmath.AngularVelocity, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return spatialmath.AngularVelocity{}, lastError
	}

	return spatialmath.AngularVelocity{}, nil
}

// CompassHeading passthrough.
func (g *rtkSerialNoNetwork) CompassHeading(ctx context.Context, extra map[string]interface{}) (float64, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return 0, lastError
	}
	return 0, nil
}

// Orientation passthrough.
func (g *rtkSerialNoNetwork) Orientation(ctx context.Context, extra map[string]interface{}) (spatialmath.Orientation, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return spatialmath.NewZeroOrientation(), lastError
	}
	return spatialmath.NewZeroOrientation(), nil
}

// ReadFix passthrough.
func (g *rtkSerialNoNetwork) ReadFix(ctx context.Context) (int, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return 0, lastError
	}

	return g.nmeamovementsensor.ReadFix(ctx)
}

// Properties passthrough.
func (g *rtkSerialNoNetwork) Properties(ctx context.Context, extra map[string]interface{}) (*movementsensor.Properties, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return &movementsensor.Properties{}, lastError
	}

	return g.nmeamovementsensor.Properties(ctx, extra)
}

// Accuracy passthrough.
func (g *rtkSerialNoNetwork) Accuracy(ctx context.Context, extra map[string]interface{}) (map[string]float32, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return map[string]float32{}, lastError
	}

	return g.nmeamovementsensor.Accuracy(ctx, extra)
}

// Readings will use the default MovementSensor Readings if not provided.
func (g *rtkSerialNoNetwork) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {

	readings := make(map[string]interface{})

	fix, err := g.ReadFix(ctx)
	if err != nil {
		return nil, err
	}

	readings["fix"] = fix

	return readings, nil
}

// Close shuts down the RTKSerialNoNetwork.
func (g *rtkSerialNoNetwork) Close(ctx context.Context) error {
	g.cancelFunc()

	if err := g.nmeamovementsensor.Close(ctx); err != nil {
		return err
	}

	g.correctionReaderMu.Lock()

	//close the reader
	if g.correctionReader != nil {
		if err := g.correctionReader.Close(); err != nil {
			g.correctionReaderMu.Unlock()
			return err
		}
		g.correctionReader = nil
	}

	g.correctionReaderMu.Unlock()

	// close the writer
	if g.correctionWriter != nil {
		if err := g.correctionWriter.Close(); err != nil {
			return err
		}
		g.correctionWriter = nil
	}

	g.activeBackgroundWorkers.Wait()

	if err := g.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
