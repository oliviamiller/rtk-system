package gpsrtkserialnonetwork

import (
	"context"
	"testing"

	"github.com/edaniels/golog"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
	"go.viam.com/utils"
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
				SerialNMEAPath:       "some-path",
				SerialCorrectionPath: "some-path2"},
		},
		{
			name: "a config with no serial_nmea_path should result in error",
			config: &Config{
				SerialCorrectionPath: "some-path2",
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "serial_nmea_path"),
		},
		{
			name: "a config with no serial_correction_path should result in error",
			config: &Config{
				SerialNMEAPath: "some-path2",
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

func TestNewrtkSerialNoNetwork(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()
	deps := make(resource.Dependencies)
	tests := []struct {
		name        string
		config      resource.Config
		expectedErr error
	}{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conf, err := resource.NativeConfig[*Config](tc.config)
			test.That(t, err, test.ShouldBeNil)
			gps, err := newrtkSerialNoNetwork(ctx, deps, tc.config.ResourceName(), conf, logger)
			if tc.expectedErr == nil {
				test.That(t, err, test.ShouldNotBeNil)
			}
		})
	}
}
