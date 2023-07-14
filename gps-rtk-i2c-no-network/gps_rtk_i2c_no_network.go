package gpsrtki2c

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"
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

var errNilLocation = errors.New("nil gps location, check nmea message parsing")
var Model = resource.NewModel("viam-labs", "movement-sensor", "gps-rtk-i2c-no-network")

type Config struct {
	I2CBus      int `json:"i2c_bus"`
	NMEAAddr    int `json:"nmea_i2c_addr"` // address of the rover
	RTCMAddr    int `json:"rtcm_i2c_addr"` // address of the station
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
	if cfg.RTCMAddr == 0 {
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

	data gpsnmea.GPSData
	mu   sync.RWMutex

	bus       int
	wbaud     int
	readAddr  byte
	writeAddr byte

	readI2c  *i2c.I2C
	writeI2c *i2c.I2C
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

	g.wbaud = newConf.I2CBaudRate
	g.readAddr = byte(newConf.RTCMAddr)
	g.writeAddr = byte(newConf.NMEAAddr)
	g.bus = newConf.I2CBus

	if err := g.start(); err != nil {
		return nil, err
	}
	return g, g.err.Get()
}

// Start begins the background task to recieve and write I2C.
func (g *RTKI2CNoNetwork) start() error {
	if err := g.startGPSNMEA(g.cancelCtx); err != nil {
		g.lastposition.GetLastPosition()
		return err
	}

	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() { g.receiveAndWriteI2C(g.cancelCtx) })

	return g.err.Get()
}

// start begins reading nmea messages from module and updates gps data.
func (g *RTKI2CNoNetwork) startGPSNMEA(ctx context.Context) error {

	err := g.initializeI2C(ctx)
	if err != nil {
		g.logger.Errorf("error initializing i2c %v", err)
		g.err.Set(err)
	}

	g.activeBackgroundWorkers.Add(1)
	utils.PanicCapturingGo(func() {
		g.readNMEAMessages(ctx)
	})

	return g.err.Get()
}

func (g *RTKI2CNoNetwork) readNMEAMessages(ctx context.Context) {
	defer g.activeBackgroundWorkers.Done()
	strBuf := ""
	for {
		select {
		case <-g.cancelCtx.Done():
			return
		default:
		}
		// open/close each loop so other things also have a chance to use i2c
		// create i2c connection
		i2cBus, err := i2c.NewI2C(g.writeAddr, g.bus)
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
}

func (g *RTKI2CNoNetwork) initializeI2C(ctx context.Context) error {

	// create i2c connection
	i2cBus, err := i2c.NewI2C(g.writeAddr, g.bus)
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
		g.logger.Errorf("Failed to set baud rate")
	}
	_, err = i2cBus.WriteBytes(cmd314)
	if err != nil {
		g.logger.Errorf("i2c write failed %s", err)
		return err
	}
	_, err = i2cBus.WriteBytes(cmd220)
	if err != nil {
		g.logger.Errorf("i2c write failed %s", err)
		return err
	}
	err = i2cBus.Close()
	if err != nil {
		g.logger.Errorf("failed to close handle: %s", err)
		return err
	}
	return nil
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

// LinearVelocity passthrough.
func (g *RTKI2CNoNetwork) LinearVelocity(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return r3.Vector{}, lastError
	}

	g.mu.RLock()
	defer g.mu.RUnlock()
	return r3.Vector{X: 0, Y: g.data.Speed, Z: 0}, g.err.Get()
}

// LinearAcceleration not supported.
func (g *RTKI2CNoNetwork) LinearAcceleration(ctx context.Context, extra map[string]interface{}) (r3.Vector, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return r3.Vector{}, movementsensor.ErrMethodUnimplementedLinearAcceleration

}

// AngularVelocity not supported.
func (g *RTKI2CNoNetwork) AngularVelocity(ctx context.Context, extra map[string]interface{}) (spatialmath.AngularVelocity, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return spatialmath.AngularVelocity{}, movementsensor.ErrMethodUnimplementedAngularVelocity
}

// CompassHeading not supported.
func (g *RTKI2CNoNetwork) CompassHeading(ctx context.Context, extra map[string]interface{}) (float64, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return 0, movementsensor.ErrMethodUnimplementedCompassHeading
}

// Orientation not supported.
func (g *RTKI2CNoNetwork) Orientation(ctx context.Context, extra map[string]interface{}) (spatialmath.Orientation, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return nil, movementsensor.ErrMethodUnimplementedOrientation
}

// ReadFix passthrough.
func (g *RTKI2CNoNetwork) readFix(ctx context.Context) (int, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return 0, lastError
	}

	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.data.FixQuality, g.err.Get()
}

// Properties passthrough.
func (g *RTKI2CNoNetwork) Properties(ctx context.Context, extra map[string]interface{}) (*movementsensor.Properties, error) {
	return &movementsensor.Properties{
		LinearVelocitySupported: true,
		PositionSupported:       true,
	}, nil
}

// Accuracy passthrough.
func (g *RTKI2CNoNetwork) Accuracy(ctx context.Context, extra map[string]interface{}) (map[string]float32, error) {
	lastError := g.err.Get()
	if lastError != nil {
		return map[string]float32{}, lastError
	}

	g.mu.RLock()
	defer g.mu.RUnlock()
	return map[string]float32{"hDOP": float32(g.data.HDOP), "vDOP": float32(g.data.VDOP)}, g.err.Get()
}

// Readings will use the default MovementSensor Readings if not provided.
func (g *RTKI2CNoNetwork) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	readings, err := movementsensor.Readings(ctx, g, extra)

	if err != nil {
		return nil, err
	}

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

	if err := g.err.Get(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
