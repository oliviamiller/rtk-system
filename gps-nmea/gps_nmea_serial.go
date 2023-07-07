package nmea

import (
	"bufio"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/edaniels/golog"
	"github.com/golang/geo/r3"
	"github.com/jacobsa/go-serial/serial"
	geo "github.com/kellydunn/golang-geo"
	"go.viam.com/utils"

	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
)

var errNilLocation = errors.New("nil gps location, check nmea message parsing")

// SerialNMEAMovementSensor allows the use of any MovementSensor chip that communicates over serial.
type SerialNMEAMovementSensor struct {
	resource.Named
	resource.AlwaysRebuild
	mu                      sync.RWMutex
	cancelCtx               context.Context
	cancelFunc              func()
	logger                  golog.Logger
	data                    gpsData
	activeBackgroundWorkers sync.WaitGroup

	disableNmea  bool
	err          movementsensor.LastError
	lastposition movementsensor.LastPosition

	dev      io.ReadWriteCloser
	path     string
	baudRate uint
}

// NewSerialGPSNMEA gps that communicates over serial.
func NewSerialGPSNMEA(ctx context.Context,
	name resource.Name,
	conf *Config,
	logger golog.Logger) (NmeaMovementSensor, error) {

	options := serial.OpenOptions{
		PortName:        conf.SerialPath,
		BaudRate:        uint(conf.SerialBaudRate),
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 4,
	}

	dev, err := serial.Open(options)
	if err != nil {
		return nil, err
	}

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	g := &SerialNMEAMovementSensor{
		Named:        name.AsNamed(),
		dev:          dev,
		cancelCtx:    cancelCtx,
		cancelFunc:   cancelFunc,
		logger:       logger,
		path:         conf.SerialPath,
		baudRate:     uint(conf.SerialBaudRate),
		err:          movementsensor.NewLastError(1, 1),
		lastposition: movementsensor.NewLastPosition(),
	}

	if err := g.Start(ctx); err != nil {
		g.logger.Errorf("Did not create nmea gps with err %#v", err.Error())
	}

	return g, err
}

// Start begins reading nmea messages from module and updates gps data.
func (g *SerialNMEAMovementSensor) Start(ctx context.Context) error {
	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer g.activeBackgroundWorkers.Done()
		r := bufio.NewReader(g.dev)
		for {
			select {
			case <-g.cancelCtx.Done():
				return
			default:
			}

			if !g.disableNmea {
				line, err := r.ReadString('\n')
				if err != nil {
					g.logger.Errorf("can't read gps serial %s", err)
					g.err.Set(err)
					return
				}
				// Update our struct's gps data in-place
				g.mu.Lock()
				err = g.data.parseAndUpdate(line)
				g.mu.Unlock()
				if err != nil {
					g.logger.Warnf("can't parse nmea sentence: %#v", err)
				}
			}
		}
	})

	return g.err.Get()
}

// nolint
// Position position, altitide.
func (g *SerialNMEAMovementSensor) Position(ctx context.Context, extra map[string]interface{}) (*geo.Point, float64, error) {
	lastPosition := g.lastposition.GetLastPosition()

	g.mu.RLock()
	defer g.mu.RUnlock()

	currentPosition := g.data.location

	if currentPosition == nil {
		return lastPosition, 0, errNilLocation
	}

	// if current position is (0,0) we will return the last non zero position
	if g.lastposition.IsZeroPosition(currentPosition) && !g.lastposition.IsZeroPosition(lastPosition) {
		return lastPosition, g.data.alt, g.err.Get()
	}

	// updating lastposition if it is different from the current position
	if !g.lastposition.ArePointsEqual(currentPosition, lastPosition) {
		g.lastposition.SetLastPosition(currentPosition)
	}

	// updating the last known valid position if the current position is non-zero
	if !g.lastposition.IsZeroPosition(currentPosition) && !g.lastposition.IsPositionNaN(currentPosition) {
		g.lastposition.SetLastPosition(currentPosition)
	}

	return currentPosition, g.data.alt, g.err.Get()
}

// Accuracy returns the accuracy, hDOP and vDOP.
func (g *SerialNMEAMovementSensor) Accuracy(ctx context.Context, extra map[string]interface{}) (map[string]float32, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return map[string]float32{"hDOP": float32(g.data.hDOP), "vDOP": float32(g.data.vDOP)}, nil
}

// LinearVelocity linear velocity.
func (g *SerialNMEAMovementSensor) LinearVelocity(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return r3.Vector{X: 0, Y: g.data.speed, Z: 0}, nil
}

// LinearAcceleration linear acceleration.
func (g *SerialNMEAMovementSensor) LinearAcceleration(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return r3.Vector{}, movementsensor.ErrMethodUnimplementedLinearAcceleration
}

// AngularVelocity angularvelocity.
func (g *SerialNMEAMovementSensor) AngularVelocity(ctx context.Context, extra map[string]interface{}) (spatialmath.AngularVelocity, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return spatialmath.AngularVelocity{}, movementsensor.ErrMethodUnimplementedAngularVelocity
}

// Orientation orientation.
func (g *SerialNMEAMovementSensor) Orientation(ctx context.Context, extra map[string]interface{}) (spatialmath.Orientation, error) {
	return spatialmath.NewOrientationVector(), movementsensor.ErrMethodUnimplementedOrientation
}

// CompassHeading 0->360.
func (g *SerialNMEAMovementSensor) CompassHeading(ctx context.Context, extra map[string]interface{}) (float64, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return 0, movementsensor.ErrMethodUnimplementedCompassHeading
}

// ReadFix returns Fix quality of MovementSensor measurements.
func (g *SerialNMEAMovementSensor) ReadFix(ctx context.Context) (int, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.data.fixQuality, nil
}

// Readings will use return all of the MovementSensor Readings.
func (g *SerialNMEAMovementSensor) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	readings, err := movementsensor.Readings(ctx, g, extra)
	if err != nil {
		return nil, err
	}

	fix, err := g.ReadFix(ctx)
	if err != nil {
		return nil, err
	}

	readings["fix"] = fix

	return readings, nil
}

// Properties what do I do!
func (g *SerialNMEAMovementSensor) Properties(ctx context.Context, extra map[string]interface{}) (*movementsensor.Properties, error) {
	return &movementsensor.Properties{
		LinearVelocitySupported: true,
		PositionSupported:       true,
	}, nil
}

// Close shuts down the SerialNMEAMovementSensor.
func (g *SerialNMEAMovementSensor) Close(ctx context.Context) error {
	g.logger.Debug("Closing SerialNMEAMovementSensor")
	g.cancelFunc()
	defer g.activeBackgroundWorkers.Wait()

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dev != nil {
		if err := g.dev.Close(); err != nil {
			return err
		}
		g.dev = nil
		g.logger.Debug("SerialNMEAMovementSensor Closed")
	}
	return nil
}