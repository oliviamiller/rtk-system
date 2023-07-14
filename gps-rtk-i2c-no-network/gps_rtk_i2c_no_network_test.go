package gpsrtki2c

import (
	"context"
	"testing"

	"github.com/edaniels/golog"
	geo "github.com/kellydunn/golang-geo"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/movementsensor/gpsnmea"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
	"go.viam.com/utils"
)

const (
	testi2cBus   = 1
	testNmeaAddr = 66
	testRTCMAddr = 67
)

func TestValidate(t *testing.T) {
	path := "path"

	tests := []struct {
		name        string
		config      *Config
		expectedErr error
	}{
		{
			name: "A valid config should result in no errors",
			config: &Config{
				I2CBus:   testi2cBus,
				NMEAAddr: testNmeaAddr,
				RTCMAddr: testRTCMAddr,
			},
		},
		{
			name: "a config with no i2c_bus should result in error",
			config: &Config{
				NMEAAddr: testNmeaAddr,
				RTCMAddr: testRTCMAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "i2c_bus"),
		},
		{
			name: "a config with no nmeaAddr should result in error",
			config: &Config{
				I2CBus:   testi2cBus,
				RTCMAddr: testRTCMAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "nmea_i2c_addr"),
		},
		{
			name: "a config with no rtcmAddr should result in error",
			config: &Config{
				I2CBus:   testi2cBus,
				NMEAAddr: testNmeaAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "rtcm_i2c_addr"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps, err := tc.config.Validate(path)
			if tc.expectedErr != nil {
				test.That(t, err, test.ShouldBeError, tc.expectedErr)
				test.That(t, len(deps), test.ShouldEqual, 0)
			} else {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, len(deps), test.ShouldEqual, 0)
			}
		})
	}
}

func TestNewrtki2cNoNetwork(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()
	deps := make(resource.Dependencies)
	//c := make(chan []uint8)
	tests := []struct {
		name           string
		resourceConfig resource.Config
		config         *Config
		expectedErr    error
	}{
		{
			name: "A valid config should successfully create new movementsensor",
			resourceConfig: resource.Config{
				Name:  "movementsensor1",
				Model: Model,
				API:   movementsensor.API,
			},
			config: &Config{
				I2CBus:   testi2cBus,
				NMEAAddr: testNmeaAddr,
				RTCMAddr: testRTCMAddr,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := newRTKI2CNoNetwork(ctx, deps, tc.resourceConfig.ResourceName(), tc.config, logger)
			if tc.expectedErr == nil {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, g.Close(context.Background()), test.ShouldBeNil)
				test.That(t, g, test.ShouldNotBeNil)
				test.That(t, g.Name(), test.ShouldResemble, tc.resourceConfig.ResourceName())
			}
		})
	}
}

func TestPosition(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()

	mockGPSData := gpsnmea.GPSData{
		Location:   geo.NewPoint(1, 2),
		Alt:        3,
		Speed:      4,
		VDOP:       5,
		HDOP:       6,
		SatsInView: 7,
		SatsInUse:  8,
		FixQuality: 5,
	}

	lastPostion := movementsensor.LastPosition{}
	lastPostion.SetLastPosition(geo.NewPoint(2, 1))

	rtk := &rtkI2CNoNetwork{
		logger:    logger,
		cancelCtx: ctx,
		data:      mockGPSData,
	}

	tests := []struct {
		name          string
		location      *geo.Point
		validLocation bool
	}{
		{
			name:          "should return the current postion",
			location:      geo.NewPoint(1, 2),
			validLocation: true,
		},
		{
			name:          "if the current location is zero should return the last known position",
			location:      geo.NewPoint(0, 0),
			validLocation: false,
		},
		{
			name:          "if current location is nil return the last known position",
			validLocation: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockGPSData.Location = tc.location
			location, alt, err := rtk.Position(ctx, nil)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, alt, test.ShouldEqual, mockGPSData.Alt)

			if tc.validLocation {
				test.That(t, location, test.ShouldResemble, mockGPSData.Location)
			}

			// last position should be updated to the most recent known position
			test.That(t, location, test.ShouldEqual, rtk.lastposition.GetLastPosition())

		})
	}
}
