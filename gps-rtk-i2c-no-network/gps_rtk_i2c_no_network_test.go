package gpsrtki2c

import (
	"context"
	"testing"

	"github.com/edaniels/golog"
	"go.viam.com/rdk/components/movementsensor"
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
