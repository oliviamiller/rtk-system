package gpsrtkserialnonetwork

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/edaniels/golog"
	"github.com/golang/geo/r3"
	geo "github.com/kellydunn/golang-geo"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/movementsensor/gpsnmea"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
	"go.viam.com/utils"
)

const nmeaPath = "nmea-path"
const correctionPath = "corr-path"

var mockGPSData = gpsnmea.GPSData{
	Location:   geo.NewPoint(1, 2),
	Alt:        3,
	Speed:      4,
	VDOP:       5,
	HDOP:       6,
	SatsInView: 7,
	SatsInUse:  8,
	FixQuality: 5,
}

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
				SerialNMEAPath:       nmeaPath,
				SerialCorrectionPath: correctionPath},
		},
		{
			name: "a config with no serial_nmea_path should result in error",
			config: &Config{
				SerialCorrectionPath: correctionPath,
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "serial_nmea_path"),
		},
		{
			name: "a config with no serial_correction_path should result in error",
			config: &Config{
				SerialNMEAPath: nmeaPath,
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
	c := make(chan []uint8)
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
				SerialNMEAPath:       nmeaPath,
				SerialCorrectionPath: correctionPath,
				TestChan:             c,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := newrtkSerialNoNetwork(ctx, deps, tc.resourceConfig.ResourceName(), tc.config, logger)
			if tc.expectedErr == nil {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, g.Name(), test.ShouldResemble, tc.resourceConfig.ResourceName())
				test.That(t, g.Close(context.Background()), test.ShouldBeNil)
				test.That(t, g, test.ShouldNotBeNil)
			}
		})
	}
}

func TestPosition(t *testing.T) {

	var logger = golog.NewTestLogger(t)
	var ctx = context.Background()

	var testRTK = &rtkSerialNoNetwork{
		logger:    logger,
		cancelCtx: ctx,
		data:      mockGPSData,
	}

	lastPostion := movementsensor.LastPosition{}
	lastPostion.SetLastPosition(geo.NewPoint(2, 1))

	tests := []struct {
		name          string
		location      *geo.Point
		validLocation bool
		expectedErr   error
	}{
		{
			name:          "should return the current postion",
			location:      geo.NewPoint(1, 2),
			validLocation: true,
		},
		{
			name:          "should return the current postion with floating point close to zero",
			location:      geo.NewPoint(0.01, 0.001),
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
			expectedErr:   errNilLocation,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testRTK.data.Location = tc.location
			location, alt, err := testRTK.Position(ctx, nil)
			if tc.expectedErr == nil {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, alt, test.ShouldEqual, mockGPSData.Alt)
			} else {
				test.That(t, err, test.ShouldBeError, tc.expectedErr)
				test.That(t, alt, test.ShouldEqual, 0)
			}
			if tc.validLocation {
				test.That(t, location, test.ShouldResemble, tc.location)
			}
			// last position should be updated to the most recent known position
			test.That(t, location, test.ShouldEqual, testRTK.lastposition.GetLastPosition())

		})
	}
}

func TestLinearVelocity(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()

	testRTK := &rtkSerialNoNetwork{
		logger:    logger,
		cancelCtx: ctx,
		data:      mockGPSData,
	}

	linearVel, err := testRTK.LinearVelocity(ctx, nil)
	test.That(t, err, test.ShouldBeNil)

	// The Y value of the vector should be the speed in GPSData.
	test.That(t, linearVel.Y, test.ShouldResemble, mockGPSData.Speed)
	test.That(t, linearVel.X, test.ShouldBeZeroValue)
	test.That(t, linearVel.Z, test.ShouldBeZeroValue)
}

func TestLinearAcceleration(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()

	testRTK := &rtkSerialNoNetwork{
		logger:    logger,
		cancelCtx: ctx,
		data:      mockGPSData,
	}

	linearAcc, err := testRTK.LinearAcceleration(ctx, nil)
	test.That(t, err, test.ShouldEqual, movementsensor.ErrMethodUnimplementedLinearAcceleration)
	test.That(t, linearAcc, test.ShouldResemble, r3.Vector{})
}

func TestReadFix(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()

	testRTK := &rtkSerialNoNetwork{
		logger:    logger,
		cancelCtx: ctx,
		data:      mockGPSData,
	}

	fix, err := testRTK.readFix(ctx)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, fix, test.ShouldEqual, mockGPSData.FixQuality)

}

func TestClose(t *testing.T) {
	logger := golog.NewTestLogger(t)
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	r := io.NopCloser(strings.NewReader("hello world"))
	var w io.ReadWriteCloser

	testRTK := &rtkSerialNoNetwork{
		logger:           logger,
		cancelCtx:        cancelCtx,
		cancelFunc:       cancelFunc,
		data:             mockGPSData,
		err:              movementsensor.NewLastError(1, 1),
		correctionReader: r,
		correctionWriter: w,
	}

	err := testRTK.Close(cancelCtx)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, testRTK.correctionReader, test.ShouldBeNil)
}
