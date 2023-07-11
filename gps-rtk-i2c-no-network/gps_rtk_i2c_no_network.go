package gpsrtki2c

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"
	"github.com/edaniels/golog"
	"github.com/golang/geo/r3"
	geo "github.com/kellydunn/golang-geo"
	"go.viam.com/utils"

	nmea "rtksystem/gps-nmea"

	"go.viam.com/rdk/components/movementsensor"
	gpsnmea "go.viam.com/rdk/components/movementsensor/gpsnmea"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
)

var Model = resource.NewModel("viam-labs", "movement-sensor", "gps-rtk-i2c-no-network")

const i2cStr = "i2c"

type Config struct {
	I2CBus      int `json:"i2c_bus"`
	NMEAAddr    int `json:"nmea_i2c_addr"`
	RCTMAddr    int `json:"rctm_i2c_addr"`
	I2CBaudRate int `json:"i2c_baud_rate,omitempty"`
}

// Validate ensures all parts of the config are valid.
func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.I2CBus == 0 {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "i2c_bus")
	}
	if cfg.NMEAAddr == 0 {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "nmea_i2c_addr")
	}
	if cfg.RCTMAddr == 0 {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "rctm_i2c_addr")
	}
	return []string{}, nil
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
				return newRTKI2CNoNetwork(ctx, deps, conf.ResourceName(), newConf, logger)
			},
		})
}

// A RTKMovementSensor is an NMEA MovementSensor model that can intake RTK correction data.
type RTKI2CNoNetwork struct {
	resource.Named
	resource.AlwaysRebuild
	logger     golog.Logger
	cancelCtx  context.Context
	cancelFunc func()

	activeBackgroundWorkers sync.WaitGroup

	err          movementsensor.LastError
	lastposition movementsensor.LastPosition

	Nmeamovementsensor gpsnmea.NmeaMovementSensor

	bus       int
	wbaud     int
	readAddr  byte
	writeAddr byte
	readI2c   *i2c.I2C
	writeI2c  *i2c.I2C
}

func newRTKI2CNoNetwork(
	ctx context.Context,
	deps resource.Dependencies,
	name resource.Name,
	newConf *Config,
	logger golog.Logger,
) (movementsensor.MovementSensor, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	g := &RTKI2CNoNetwork{
		Named:        name.AsNamed(),
		cancelCtx:    cancelCtx,
		cancelFunc:   cancelFunc,
		logger:       logger,
		err:          movementsensor.NewLastError(1, 1),
		lastposition: movementsensor.NewLastPosition(),
	}

	if newConf.I2CBaudRate == 0 {
		newConf.I2CBaudRate = 38400
		g.logger.Info("using default baud rate 38400")
	}

	// Init NMEAMovementSensor
	var err error
	nmeaConf := &nmea.Config{I2CBus: newConf.I2CBus, I2cAddr: newConf.NMEAAddr}
	g.Nmeamovementsensor, err = nmea.NewNMEAGPS(ctx, deps, name, nmeaConf, logger)
	if err != nil {
		return nil, err
	}
	g.wbaud = newConf.I2CBaudRate
	g.readAddr = byte(newConf.RCTMAddr)
	g.writeAddr = byte(newConf.NMEAAddr)
	g.bus = newConf.I2CBus

	if err := g.start(); err != nil {
		return nil, err
	}
	return g, g.err.Get()
}

// Start begins the background task to recieve and write I2C.
func (g *RTKI2CNoNetwork) start() error {
	if err := g.Nmeamovementsensor.Start(g.cancelCtx); err != nil {
		g.lastposition.GetLastPosition()
		return err
	}

	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() { g.receiveAndWriteI2C(g.cancelCtx) })

	return g.err.Get()
}

// receiveAndWriteI2C reads tbe rctm correction messages from the read addr and writes the write addr
func (g *RTKI2CNoNetwork) receiveAndWriteI2C(ctx context.Context) {

	defer g.activeBackgroundWorkers.Done()
	if err := g.cancelCtx.Err(); err != nil {
		return
	}

	var err error
	for err == nil {
		select {
		case <-g.cancelCtx.Done():
			return
		default:
		}
		var rctmData []byte

		// create i2c connections
		var err error
		g.readI2c, err = i2c.NewI2C(g.readAddr, g.bus)
		g.err.Set(err)

		g.writeI2c, err = i2c.NewI2C(g.writeAddr, g.bus)
		g.err.Set(err)

		// change so you don't see a million logs
		logger.ChangePackageLogLevel("i2c", logger.InfoLevel)

		buf := make([]byte, 1024)
		_, err = g.readI2c.ReadBytes(buf)
		g.err.Set(err)
		if err != nil {
			g.logger.Debug("Could not read from the i2c address")
		}

		// write only the rctm data
		for _, b := range buf {
			if b != 255 {
				rctmData = append(rctmData, b)
			}
		}

		if len(rctmData) != 0 {
			_, err = g.writeI2c.WriteBytes(rctmData)
			g.err.Set(err)
			if err != nil {
				g.logger.Debug("Could not write to i2c address")
			}
		}

		// close I2C handles each time so other processes can use them
		err = g.readI2c.Close()
		g.err.Set(err)
		if err != nil {
			g.logger.Debug("failed to close i2c handle: %s", err)
			return
		}
		err = g.writeI2c.Close()
		g.err.Set(err)
		if err != nil {
			g.logger.Debug("failed to close i2c handle: %s", err)
			return
		}
	}
}

// Position returns the current geographic location of the MOVEMENTSENSOR.
func (g *RTKI2CNoNetwork) Position(ctx context.Context, extra map[string]interface{}) (*geo.Point, float64, error) {
	lastError := g.err.Get()
	if lastError != nil {
		lastPosition := g.lastposition.GetLastPosition()
		if lastPosition != nil {
			return lastPosition, 0, nil
		}
		return geo.NewPoint(math.NaN(), math.NaN()), math.NaN(), lastError
	}

	position, alt, err := g.Nmeamovementsensor.Position(ctx, extra)
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
func (g *RTKI2CNoNetwork) LinearVelocity(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return r3.Vector{}, lastError
	}

	return g.Nmeamovementsensor.LinearVelocity(ctx, extra)
}

// LinearAcceleration passthrough.
func (g *RTKI2CNoNetwork) LinearAcceleration(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return r3.Vector{}, lastError
	}
	return g.Nmeamovementsensor.LinearAcceleration(ctx, extra)
}

// AngularVelocity passthrough.
func (g *RTKI2CNoNetwork) AngularVelocity(ctx context.Context, extra map[string]interface{}) (spatialmath.AngularVelocity, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return spatialmath.AngularVelocity{}, lastError
	}

	return g.Nmeamovementsensor.AngularVelocity(ctx, extra)
}

// CompassHeading passthrough.
func (g *RTKI2CNoNetwork) CompassHeading(ctx context.Context, extra map[string]interface{}) (float64, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return 0, lastError
	}

	return g.Nmeamovementsensor.CompassHeading(ctx, extra)
}

// Orientation passthrough.
func (g *RTKI2CNoNetwork) Orientation(ctx context.Context, extra map[string]interface{}) (spatialmath.Orientation, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return spatialmath.NewZeroOrientation(), lastError
	}

	return g.Nmeamovementsensor.Orientation(ctx, extra)
}

// ReadFix passthrough.
func (g *RTKI2CNoNetwork) ReadFix(ctx context.Context) (int, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return 0, lastError
	}

	return g.Nmeamovementsensor.ReadFix(ctx)
}

// Properties passthrough.
func (g *RTKI2CNoNetwork) Properties(ctx context.Context, extra map[string]interface{}) (*movementsensor.Properties, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return &movementsensor.Properties{}, lastError
	}

	return g.Nmeamovementsensor.Properties(ctx, extra)
}

// Accuracy passthrough.
func (g *RTKI2CNoNetwork) Accuracy(ctx context.Context, extra map[string]interface{}) (map[string]float32, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return map[string]float32{}, lastError
	}

	return g.Nmeamovementsensor.Accuracy(ctx, extra)
}

// Readings will use the default MovementSensor Readings if not provided.
func (g *RTKI2CNoNetwork) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
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

// Close shuts down the RTKI2CNoNetwork.
func (g *RTKI2CNoNetwork) Close(ctx context.Context) error {

	g.cancelFunc()

	if err := g.readI2c.Close(); err != nil {
		return err
	}

	if err := g.readI2c.Close(); err != nil {
		return err
	}

	if err := g.Nmeamovementsensor.Close(ctx); err != nil {
		return err
	}

	if err := g.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
