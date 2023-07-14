package stationi2c

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
	testBus         = 1
	testi2cAddr     = 44
	testStationName = "testStation"
	path            = "path"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectedErr error
	}{
		{
			name: "A valid config with i2c connection should result in no errors",
			config: &Config{
				RequiredAccuracy: 4,
				RequiredTime:     200,
				I2CBus:           testBus,
				I2CAddr:          testi2cAddr,
			},
		},
		{
			name: "a config with no RequiredAccuracy should result in error",
			config: &Config{
				RequiredTime: 200,
				I2CBus:       testBus,
				I2CAddr:      testi2cAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "required_accuracy"),
		},
		{
			name: "a config with no RequiredTime should result in error",
			config: &Config{
				RequiredAccuracy: 4,
				I2CBus:           testBus,
				I2CAddr:          testi2cAddr,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "required_time"),
		},
		{
			name: "The required accuracy can only be values 1-5",
			config: &Config{
				RequiredAccuracy: 6,
				RequiredTime:     200,
				I2CBus:           testBus,
				I2CAddr:          testi2cAddr,
			},
			expectedErr: errRequiredAccuracy,
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

func TestNewRTKStationI2C(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()
	deps := make(resource.Dependencies)

	c := make(chan []byte, 1024)

	tests := []struct {
		name         string
		resourceConf *resource.Config
		conf         *Config
		expectedErr  error
	}{
		{
			name: "A valid config with i2c connection should result in no errors",
			resourceConf: &resource.Config{
				Name:  testStationName,
				Model: Model,
				API:   movementsensor.API,
			},
			conf: &Config{
				RequiredAccuracy: 4,
				RequiredTime:     200,
				I2CBus:           testBus,
				I2CAddr:          testi2cAddr,
				TestChan:         c,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := newRTKStationI2C(ctx, deps, tc.resourceConf.ResourceName(), tc.conf, logger)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, g.Name(), test.ShouldResemble, tc.resourceConf.ResourceName())
		})
	}
}
