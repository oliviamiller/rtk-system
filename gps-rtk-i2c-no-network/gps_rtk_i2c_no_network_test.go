package gpsrtki2c

import (
	"testing"

	"go.viam.com/test"
	"go.viam.com/utils"
)

const (
	testi2cBus   = 1
	testNmeaAddr = 66
	testRCTMAddr = 67
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
				RCTMAddr: testRCTMAddr,
			},
		},
		{
			name: "a config with no i2c_bus should result in error",
			config: &Config{
				NMEAAddr: testNmeaAddr,
				RCTMAddr: testRCTMAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "i2c_bus"),
		},
		{
			name: "a config with no nmeaAddr should result in error",
			config: &Config{
				I2CBus:   testi2cBus,
				RCTMAddr: testRCTMAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "serial_correction_path"),
		},
		{
			name: "a config with no rctmAddr should result in error",
			config: &Config{
				I2CBus:   testi2cBus,
				NMEAAddr: testNmeaAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "serial_correction_path"),
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
