// Package nmea implements the NMEA gps sensors for the no-network rtk models.
package nmea

import (
	"context"
	"fmt"
	"sync"

	"github.com/d2r2/go-i2c"
	gologger "github.com/d2r2/go-logger"
	"github.com/edaniels/golog"
	"github.com/golang/geo/r3"
	geo "github.com/kellydunn/golang-geo"
	"go.viam.com/utils"

	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/movementsensor/gpsnmea"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
)

func NewI2CGPSNMEA(
	ctx context.Context,
	deps resource.Dependencies,
	name resource.Name,
	conf *Config,
	logger golog.Logger,
) (NmeaMovementSensor, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	g := &I2CNMEAMovementSensor{
		Named:      name.AsNamed(),
		bus:        conf.I2CBus,
		addr:       byte(conf.I2cAddr),
		wbaud:      conf.I2CBaudRate,
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
		logger:     logger,
		// Overloaded boards can have flaky I2C busses. Only report errors if at least 5 of the
		// last 10 attempts have failed.
		err:          movementsensor.NewLastError(10, 5),
		lastposition: movementsensor.NewLastPosition(),
	}

	if err := g.Start(ctx); err != nil {
		return nil, err
	}
	return g, g.err.Get()

}

type I2CNMEAMovementSensor struct {
	resource.Named
	resource.AlwaysRebuild
	mu                      sync.RWMutex
	cancelCtx               context.Context
	cancelFunc              func()
	logger                  golog.Logger
	data                    gpsnmea.GPSData
	activeBackgroundWorkers sync.WaitGroup

	disableNmea  bool
	err          movementsensor.LastError
	lastposition movementsensor.LastPosition

	bus   int
	addr  byte
	wbaud int
}

// Start begins reading nmea messages from module and updates gps data.
func (g *I2CNMEAMovementSensor) Start(ctx context.Context) error {

	// create i2c connection
	i2cBus, err := i2c.NewI2C(g.addr, g.bus)
	if err != nil {
		g.logger.Errorf("error opening the i2c bus: %v", err)
		g.err.Set(err)
	}

	// change so you don't see a million logs
	gologger.ChangePackageLogLevel("i2c", gologger.InfoLevel)

	// Send GLL, RMC, VTG, GGA, GSA, and GSV sentences each 1000ms
	baudcmd := fmt.Sprintf("PMTK251,%d", g.wbaud)
	cmd251 := movementsensor.PMTKAddChk([]byte(baudcmd))
	cmd314 := movementsensor.PMTKAddChk([]byte("PMTK314,1,1,1,1,1,1,0,0,0,0,0,0,0,0,0,0,0,0,0"))
	cmd220 := movementsensor.PMTKAddChk([]byte("PMTK220,1000"))

	_, err = i2cBus.WriteBytes(cmd251)
	if err != nil {
		g.logger.Debug("Failed to set baud rate")
	}
	_, err = i2cBus.WriteBytes(cmd314)
	if err != nil {
		g.logger.Errorf("i2c handle write failed %s", err)
		return err
	}
	_, err = i2cBus.WriteBytes(cmd220)
	if err != nil {
		g.logger.Errorf("i2c handle write failed %s", err)
		return err
	}
	err = i2cBus.Close()
	if err != nil {
		g.logger.Errorf("failed to close handle: %s", err)
		return err
	}

	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		defer g.activeBackgroundWorkers.Done()
		strBuf := ""
		for {
			select {
			case <-g.cancelCtx.Done():
				return
			default:
			}
			// Opening an i2c handle blocks the whole bus, so we open/close each loop so other things also have a chance to use it
			// create i2c connection
			i2cBus, err := i2c.NewI2C(g.addr, g.bus)
			if err != nil {
				g.logger.Errorf("error opening the i2c bus: %v", err)
				g.err.Set(err)
			}

			// change so you don't see a million logs
			gologger.ChangePackageLogLevel("i2c", gologger.InfoLevel)

			// Record the error value no matter what. If it's nil, this will help suppress
			// ephemeral errors later.
			g.err.Set(err)
			if err != nil {
				g.logger.Errorf("can't open gps i2c handle: %s", err)
				return
			}
			buffer := make([]byte, 1024)
			_, err = i2cBus.ReadBytes(buffer)
			g.err.Set(err)
			hErr := i2cBus.Close()
			g.err.Set(hErr)
			if hErr != nil {
				g.logger.Errorf("failed to close the i2c bus: %s", hErr)
				return
			}
			if err != nil {
				g.logger.Error(err)
				continue
			}
			for _, b := range buffer {
				// PMTK uses CRLF line endings to terminate sentences, but just LF to blank data.
				// Since CR should never appear except at the end of our sentence, we use that to determine sentence end.
				// LF is merely ignored.
				if b == 0x0D {
					if strBuf != "" {
						g.mu.Lock()
						err = g.data.ParseAndUpdate(strBuf)
						g.mu.Unlock()
						if err != nil {
							g.logger.Debugf("can't parse nmea : %s, %v", strBuf, err)
						}
					}
					strBuf = ""
				} else if b != 0x0A && b != 0xFF { // adds only valid bytes
					strBuf += string(b)
				}
			}
		}
	})

	return g.err.Get()
}

// nolint
// Position returns the current geographic location of the MovementSensor.
func (g *I2CNMEAMovementSensor) Position(ctx context.Context, extra map[string]interface{}) (*geo.Point, float64, error) {
	lastPosition := g.lastposition.GetLastPosition()

	g.mu.RLock()
	defer g.mu.RUnlock()

	currentPosition := g.data.Location

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

// Accuracy returns the accuracy, hDOP and vDOP.
func (g *I2CNMEAMovementSensor) Accuracy(ctx context.Context, extra map[string]interface{}) (map[string]float32, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return map[string]float32{"hDOP": float32(g.data.HDOP), "vDOP": float32(g.data.VDOP)}, g.err.Get()
}

// LinearVelocity returns the current speed of the MovementSensor.
func (g *I2CNMEAMovementSensor) LinearVelocity(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return r3.Vector{X: 0, Y: g.data.Speed, Z: 0}, g.err.Get()
}

// LinearAcceleration returns the current linear acceleration of the MovementSensor.
func (g *I2CNMEAMovementSensor) LinearAcceleration(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return r3.Vector{}, movementsensor.ErrMethodUnimplementedLinearAcceleration
}

// AngularVelocity not supported.
func (g *I2CNMEAMovementSensor) AngularVelocity(
	ctx context.Context,
	extra map[string]interface{},
) (spatialmath.AngularVelocity, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return spatialmath.AngularVelocity{}, movementsensor.ErrMethodUnimplementedAngularVelocity
}

// CompassHeading not supported.
func (g *I2CNMEAMovementSensor) CompassHeading(ctx context.Context, extra map[string]interface{}) (float64, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return 0, g.err.Get()
}

// Orientation not supporter.
func (g *I2CNMEAMovementSensor) Orientation(ctx context.Context, extra map[string]interface{}) (spatialmath.Orientation, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return nil, movementsensor.ErrMethodUnimplementedOrientation
}

// Properties what can I do!
func (g *I2CNMEAMovementSensor) Properties(ctx context.Context, extra map[string]interface{}) (*movementsensor.Properties, error) {
	return &movementsensor.Properties{
		LinearVelocitySupported: true,
		PositionSupported:       true,
	}, nil
}

// ReadFix returns quality.
func (g *I2CNMEAMovementSensor) ReadFix(ctx context.Context) (int, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.data.FixQuality, g.err.Get()
}

// Readings will use return all of the MovementSensor Readings.
func (g *I2CNMEAMovementSensor) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
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

// Close shuts down the SerialNMEAMOVEMENTSENSOR.
func (g *I2CNMEAMovementSensor) Close(ctx context.Context) error {
	g.cancelFunc()
	g.activeBackgroundWorkers.Wait()

	return g.err.Get()
}
