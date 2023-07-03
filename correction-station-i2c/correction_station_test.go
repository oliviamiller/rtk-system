package stationi2c

//TODO: RSDK-3754 fix the tests
/*const (
	testBoardName   = "board1"
	testBusName     = "i2c1"
	testi2cAddr     = 44
	testSerialPath  = "some-path"
	testStationName = "testStation"
)

var c = make(chan []byte, 1024)

func setupDependencies(t *testing.T) resource.Dependencies {
	t.Helper()

	deps := make(resource.Dependencies)

	actualBoard := inject.NewBoard(testBoardName)
	i2c1 := &inject.I2C{}
	handle1 := &inject.I2CHandle{}
	handle1.WriteFunc = func(ctx context.Context, b []byte) error {
		return nil
	}
	handle1.ReadFunc = func(ctx context.Context, count int) ([]byte, error) {
		return nil, nil
	}
	handle1.CloseFunc = func() error {
		return nil
	}
	i2c1.OpenHandleFunc = func(addr byte) (board.I2CHandle, error) {
		return handle1, nil
	}
	actualBoard.I2CByNameFunc = func(name string) (board.I2C, bool) {
		return i2c1, true
	}

	deps[board.Named(testBoardName)] = actualBoard

	return deps
}

func TestValidate(t *testing.T) {
	path := "path"
	tests := []struct {
		name          string
		stationConfig *StationConfig
		expectedErr   error
	}{
		{
			name: "A valid config with serial connection should result in no errors",
			stationConfig: &StationConfig{
				Protocol:         "serial",
				RequiredAccuracy: 4,
				RequiredTime:     200,
				SerialConfig: &SerialConfig{
					SerialPath:     "some-path",
					SerialBaudRate: 9600,
				},
				I2CConfig: &I2CConfig{},
			},
		},
		{
			name: "A valid config with i2c connection should result in no errors",
			stationConfig: &StationConfig{
				Protocol:         "i2c",
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
		{
			name: "A config without a protcol should result in error",
			stationConfig: &StationConfig{
				Protocol:         "",
				RequiredAccuracy: 4,
				RequiredTime:     200,
				SerialConfig:     &SerialConfig{},
				I2CConfig:        &I2CConfig{},
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "correction_source"),
		},
		{
			name: "a config with no RequiredAccuracy should result in error",
			stationConfig: &StationConfig{
				Protocol:         "i2c",
				RequiredAccuracy: 0,
				RequiredTime:     0,
				SerialConfig:     &SerialConfig{},
				I2CConfig:        &I2CConfig{},
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "required_accuracy"),
		},
		{
			name: "a config with no RequiredTime should result in error",
			stationConfig: &StationConfig{
				Protocol:         "i2c",
				RequiredAccuracy: 5,
				RequiredTime:     0,
				SerialConfig:     &SerialConfig{},
				I2CConfig:        &I2CConfig{},
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "required_time"),
		},
		{
			name: "The required accuracy can only be values 1-5",
			stationConfig: &StationConfig{
				Protocol:         "i2c",
				RequiredAccuracy: 6,
				RequiredTime:     200,
				SerialConfig:     &SerialConfig{},
				I2CConfig:        &I2CConfig{},
			},
			expectedErr: errRequiredAccuracy,
		},
		{
			name: "serial station without a serial correction path should result in error",
			stationConfig: &StationConfig{
				Protocol:         "serial",
				RequiredAccuracy: 5,
				RequiredTime:     200,
				SerialConfig:     &SerialConfig{},
				I2CConfig:        &I2CConfig{},
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "serial_correction_path"),
		},
		{
			name: "i2c station without a board should result in error",
			stationConfig: &StationConfig{
				Protocol:         "i2c",
				RequiredAccuracy: 5,
				RequiredTime:     200,
				SerialConfig:     &SerialConfig{},
				I2CConfig: &I2CConfig{
					I2CBus:  testBusName,
					I2cAddr: testi2cAddr,
				},
			},
			expectedErr: utils.NewConfigValidationFieldRequiredError(path, "board"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps, err := tc.stationConfig.Validate(path)

			if tc.expectedErr != nil {
				test.That(t, err, test.ShouldBeError, tc.expectedErr)
				test.That(t, len(deps), test.ShouldEqual, 0)
			} else if tc.stationConfig.Protocol == i2cStr {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, deps, test.ShouldNotBeNil)
				test.That(t, deps[0], test.ShouldEqual, testBoardName)
			}
		})
	}
}

func TestNewRTKStation(t *testing.T) {
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
					Protocol:         "serial",
					RequiredAccuracy: 4,
					RequiredTime:     200,
					SerialConfig: &SerialConfig{
						SerialPath:     "testChan",
						SerialBaudRate: 9600,
						TestChan:       c,
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
					Protocol:         "i2c",
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
					Protocol:         "serial",
					RequiredAccuracy: 4,
					RequiredTime:     200,
					SerialConfig: &SerialConfig{
						SerialPath: "some-path",
						TestChan:   c,
					},
					I2CConfig: &I2CConfig{},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			//g, err := newRTKStation(ctx, deps, tc.config, logger)
			//test.That(t, err, test.ShouldBeNil)
			//test.That(t, g.Name(), test.ShouldResemble, tc.config.ResourceName())
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
