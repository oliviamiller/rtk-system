package gpsrtkserialnonetwork

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"math"
	"sync"

	"github.com/edaniels/golog"
	"github.com/go-gnss/rtcm/rtcm3"
	"github.com/golang/geo/r3"
	slib "github.com/jacobsa/go-serial/serial"
	geo "github.com/kellydunn/golang-geo"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/movementsensor/gpsnmea"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
	"go.viam.com/utils"
)

var Model = resource.NewModel("viam-labs", "movement-sensor", "gps-rtk-serial-no-network")
var errNilLocation = errors.New("nil gps location, check nmea message parsing")

type Config struct {
	SerialNMEAPath           string `json:"serial_nmea_path"` // The path that NMEA data is being written to
	SerialNMEABaudRate       int    `json:"serial_nmea_baud_rate,omitempty"`
	SerialCorrectionPath     string `json:"serial_correction_path"` // The path that rtcm data will be read from
	SerialCorrectionBaudRate int    `json:"serial_correction_baud_rate"`

	// TestChan is a fake "serial" path for test use only
	TestChan chan []uint8 `json:"-"`
}

// ValidateSerial ensures all parts of the config are valid.
func (cfg *Config) Validate(path string) ([]string, error) {
	var deps []string
	if cfg.SerialNMEAPath == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "serial_nmea_path")
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

	data   gpsnmea.GPSData
	dataMu sync.RWMutex

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

	g.writePath = newConf.SerialNMEAPath
	g.writeBaudRate = newConf.SerialNMEABaudRate

	if g.writeBaudRate == 0 {
		g.writeBaudRate = 38400
	}

	g.readPath = newConf.SerialCorrectionPath
	g.readBaudRate = newConf.SerialCorrectionBaudRate

	if g.readBaudRate == 0 {
		g.readBaudRate = 38400
	}

	if newConf.TestChan == nil {
		if err := g.start(); err != nil {
			return nil, err
		}
	}
	return g, g.err.Get()

}

// Start begins reading the nmea data and correction source readings
func (g *rtkSerialNoNetwork) start() error {
	if err := g.startGPSNMEA(g.cancelCtx); err != nil {
		g.lastposition.GetLastPosition()
		return err
	}
	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(g.receiveAndWriteSerial)

	return g.err.Get()
}

// Start begins reading nmea messages from module and updates gps data.
func (g *rtkSerialNoNetwork) startGPSNMEA(ctx context.Context) error {
	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		g.readNMEAMessages(ctx)
	})

	return g.err.Get()
}

func (g *rtkSerialNoNetwork) readNMEAMessages(ctx context.Context) {
	defer g.activeBackgroundWorkers.Done()
	r := bufio.NewReader(g.openNMEAPath())
	for {
		select {
		case <-g.cancelCtx.Done():
			return
		default:
		}

		line, err := r.ReadString('\n')
		if err != nil {
			g.logger.Errorf("can't read gps serial %s", err)
			g.err.Set(err)
			return
		}
		// Update our struct's gps data in-place
		g.dataMu.Lock()
		err = g.data.ParseAndUpdate(line)
		g.dataMu.Unlock()
		if err != nil {
			g.logger.Warnf("can't parse nmea sentence: %#v", err)
		}
	}
}

func (g *rtkSerialNoNetwork) openNMEAPath() io.ReadWriteCloser {

	if err := g.cancelCtx.Err(); err != nil {
		return nil
	}

	g.correctionReaderMu.Lock()
	defer g.correctionReaderMu.Unlock()

	options := slib.OpenOptions{
		PortName:        g.writePath,
		BaudRate:        uint(g.writeBaudRate),
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 1,
	}

	var err error
	g.correctionWriter, err = slib.Open(options)
	if err != nil {
		g.logger.Errorf("serial.Open: %v", err)
		g.err.Set(err)
		return nil
	}

	return g.correctionWriter

}

func (g *rtkSerialNoNetwork) openCorrectionReader() io.ReadCloser {

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

	reader := g.openCorrectionReader()

	g.correctionWriter = g.openNMEAPath()

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
	lastPosition := g.lastposition.GetLastPosition()
	if lastError != nil {
		if lastPosition != nil {
			return lastPosition, 0, nil
		}
		return geo.NewPoint(math.NaN(), math.NaN()), math.NaN(), lastError
	}

	g.dataMu.RLock()
	defer g.dataMu.RUnlock()

	currentPosition := g.data.Location
	log.Println(g.data.FixQuality)

	if currentPosition == nil {
		return lastPosition, 0, errNilLocation
	}

	// if current position is (0,0) we will return the last non zero position
	if g.lastposition.IsZeroPosition(currentPosition) && !g.lastposition.IsZeroPosition(lastPosition) {
		return lastPosition, g.data.Alt, g.err.Get()
	}

	// updating lastposition if it is different from the current position
	if !g.lastposition.ArePointsEqual(currentPosition, lastPosition) {
		g.lastposition.SetLastPosition(currentPosition)
	}

	// updating the last known valid position if the current position is non-zero
	if !g.lastposition.IsZeroPosition(currentPosition) && !g.lastposition.IsPositionNaN(currentPosition) {
		g.lastposition.SetLastPosition(currentPosition)
	}

	return currentPosition, g.data.Alt, g.err.Get()
}

// LinearVelocity passthrough.
func (g *rtkSerialNoNetwork) LinearVelocity(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return r3.Vector{}, lastError
	}

	g.dataMu.RLock()
	defer g.dataMu.RUnlock()
	return r3.Vector{X: 0, Y: g.data.Speed, Z: 0}, g.err.Get()
}

// LinearAcceleration not supported.
func (g *rtkSerialNoNetwork) LinearAcceleration(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	g.dataMu.RLock()
	defer g.dataMu.RUnlock()
	return r3.Vector{}, movementsensor.ErrMethodUnimplementedLinearAcceleration
}

// AngularVelocity not supportd.
func (g *rtkSerialNoNetwork) AngularVelocity(ctx context.Context, extra map[string]interface{}) (spatialmath.AngularVelocity, error) {
	g.dataMu.RLock()
	defer g.dataMu.RUnlock()

	return spatialmath.AngularVelocity{}, movementsensor.ErrMethodUnimplementedAngularVelocity
}

// CompassHeading not supported.
func (g *rtkSerialNoNetwork) CompassHeading(ctx context.Context, extra map[string]interface{}) (float64, error) {
	g.dataMu.RLock()
	defer g.dataMu.RUnlock()
	return 0, movementsensor.ErrMethodUnimplementedCompassHeading
}

// Orientation not supported.
func (g *rtkSerialNoNetwork) Orientation(ctx context.Context, extra map[string]interface{}) (spatialmath.Orientation, error) {
	g.dataMu.RLock()
	defer g.dataMu.RUnlock()
	return spatialmath.NewZeroOrientation(), movementsensor.ErrMethodUnimplementedOrientation
}

// ReadFix passthrough.
func (g *rtkSerialNoNetwork) readFix(ctx context.Context) (int, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return 0, lastError
	}
	g.dataMu.RLock()
	defer g.dataMu.RUnlock()
	return g.data.FixQuality, g.err.Get()
}

// Properties passthrough.
func (g *rtkSerialNoNetwork) Properties(ctx context.Context, extra map[string]interface{}) (*movementsensor.Properties, error) {
	return &movementsensor.Properties{
		LinearVelocitySupported: true,
		PositionSupported:       true,
	}, nil
}

// Accuracy passthrough.
func (g *rtkSerialNoNetwork) Accuracy(ctx context.Context, extra map[string]interface{}) (map[string]float32, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return map[string]float32{}, lastError
	}

	g.dataMu.RLock()
	defer g.dataMu.RUnlock()
	return map[string]float32{"hDOP": float32(g.data.HDOP), "vDOP": float32(g.data.VDOP)}, g.err.Get()
}

// Readings will use the MovementSensor Readings
func (g *rtkSerialNoNetwork) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	readings := make(map[string]interface{})
	return readings, nil
}

// Close shuts down the RTKSerialNoNetwork.
func (g *rtkSerialNoNetwork) Close(ctx context.Context) error {
	g.cancelFunc()
	g.activeBackgroundWorkers.Wait()

	g.correctionReaderMu.Lock()

	// close the reader.
	if g.correctionReader != nil {
		if err := g.correctionReader.Close(); err != nil {
			g.correctionReaderMu.Unlock()
			g.err.Set(err)
			g.logger.Errorf("failed to close correction reader %s", err)
		}
		g.correctionReader = nil
	}

	g.correctionReaderMu.Unlock()

	// close the writer.
	if g.correctionWriter != nil {
		if err := g.correctionWriter.Close(); err != nil {
			g.err.Set(err)
			g.logger.Errorf("failed to close correction writer %s", err)
		}
		g.correctionWriter = nil
	}

	if err := g.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
