package stationserial

import (
	"testing"

	"go.viam.com/test"
	"go.viam.com/utils"
)

const testPath = "test-path"
const path = "path"

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
