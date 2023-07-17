package stationserial

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
	testPath        = "test-path"
	path            = "path"
	testStationName = "serial-station"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectedErr error
	}{
		{
			name: "A valid config should result in no errors",
			config: &Config{
				RequiredAccuracy: 4,
				RequiredTime:     200,
				SerialPath:       testPath,
			},
		},
		{
			name: "a config with no RequiredAccuracy should result in error",
			config: &Config{
				RequiredTime: 200,
				SerialPath:   testPath,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "required_accuracy"),
		},
		{
			name: "a config with no RequiredTime should result in error",
			config: &Config{
				RequiredAccuracy: 4,
				SerialPath:       testPath,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "required_time"),
		},
		{
			name: "The required accuracy can only be values 1-5",
			config: &Config{
				RequiredAccuracy: 6,
				RequiredTime:     200,
				SerialPath:       testPath,
			},
			expectedErr: errRequiredAccuracy,
		},
		{
			name: "No serial path should error",
			config: &Config{
				RequiredAccuracy: 6,
				RequiredTime:     200,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "serial_path"),
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

func TestNewSerialRTKStation(t *testing.T) {
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
			name: "A valid config should result in no errors",
			resourceConf: &resource.Config{
				Name:  testStationName,
				Model: Model,
				API:   movementsensor.API,
			},
			conf: &Config{
				RequiredAccuracy: 4,
				RequiredTime:     200,
				SerialPath:       testPath,
				TestChan:         c,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := newRTKStationSerial(ctx, deps, tc.resourceConf.ResourceName(), tc.conf, logger)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, g.Name(), test.ShouldResemble, tc.resourceConf.ResourceName())
			err = g.Close(ctx)
			test.That(t, err, test.ShouldBeNil)
		})
	}
}
