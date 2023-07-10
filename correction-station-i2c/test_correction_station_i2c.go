package stationi2c

/*
import (
	"testing"

	"go.viam.com/test"
	"go.viam.com/utils"
)

const (
	testBoardName   = "board1"
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

/* func TestNewRTKStation(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()
	deps := setupDependencies(t)

	c := make(chan []byte, 1024)

	tests := []struct {
		name        string
		config      resource.Config
		expectedErr error
	}{
		{
			name: "A valid config with serial connection should result in no errors",
			config: resource.Config{
				Name:  testStationName,
				Model: stationModel,
				API:   movementsensor.API,
				ConvertedAttributes: &StationConfig{
					CorrectionSource: "serial",
					Children:         []string{"rtk-sensor1"},
					RequiredAccuracy: 4,
					RequiredTime:     200,
					SerialConfig: &SerialConfig{
						SerialCorrectionPath:     "testChan",
						SerialCorrectionBaudRate: 9600,
						TestChan:                 c,
					},
					I2CConfig: &I2CConfig{},
				},
			},
		},
		{
			name: "A valid config with i2c connection should result in no errors",
			config: resource.Config{
				Name:  testStationName,
				Model: stationModel,
				API:   movementsensor.API,
				ConvertedAttributes: &StationConfig{
					CorrectionSource: "i2c",
					Children:         []string{"rtk-sensor2"},
					RequiredAccuracy: 4,
					RequiredTime:     200,
					SerialConfig:     &SerialConfig{},
					I2CConfig: &I2CConfig{
						Board:   testBoardName,
						I2CBus:  testBusName,
						I2cAddr: testi2cAddr,
					},
				},
			},
		},
		{
			name: "A rtk base station can send corrections to multiple children",
			config: resource.Config{
				Name:  testStationName,
				Model: stationModel,
				API:   movementsensor.API,
				ConvertedAttributes: &StationConfig{
					CorrectionSource: "serial",
					Children:         []string{"rtk-sensor1", "rtk-sensor2"},
					RequiredAccuracy: 4,
					RequiredTime:     200,
					SerialConfig: &SerialConfig{
						SerialCorrectionPath: "some-path",
						TestChan:             c,
					},
					I2CConfig: &I2CConfig{},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := newRTKStation(ctx, deps, tc.config, logger)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, g.Name(), test.ShouldResemble, tc.config.ResourceName())
		})
	}
}

func TestClose(t *testing.T) {
	logger := golog.NewTestLogger(t)
	ctx := context.Background()
	cancelCtx, cancelFunc := context.WithCancel(ctx)
	r := io.NopCloser(strings.NewReader("hello world"))

	tests := []struct {
		name        string
		baseStation *rtkStation
		expectedErr error
	}{
		{
			name: "Should close serial with no errors",
			baseStation: &rtkStation{
				cancelCtx: cancelCtx, cancelFunc: cancelFunc, logger: logger, correctionSource: &serialCorrectionSource{
					cancelCtx:        cancelCtx,
					cancelFunc:       cancelFunc,
					logger:           logger,
					correctionReader: r,
				},
			},
		},
		{
			name: "should close i2c with no errors",
			baseStation: &rtkStation{
				cancelCtx: cancelCtx, cancelFunc: cancelFunc, logger: logger, correctionSource: &i2cCorrectionSource{
					cancelCtx:        cancelCtx,
					cancelFunc:       cancelFunc,
					logger:           logger,
					correctionReader: r,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.baseStation.Close(ctx)
			test.That(t, err, test.ShouldBeNil)
		})
	}
} */
