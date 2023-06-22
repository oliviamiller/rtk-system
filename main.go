// package main is a module with correction-station component
package main

import (
	"context"
	station "rtkstation/correction-station"

	"github.com/edaniels/golog"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/utils"
)

func main() {
	utils.ContextualMain(mainWithArgs, golog.NewDevelopmentLogger("rtk-system"))
}

func mainWithArgs(ctx context.Context, args []string, logger golog.Logger) error {
	rtkStation, err := module.NewModuleFromArgs(ctx, logger)

	if err != nil {
		return err
	}
	rtkStation.AddModelFromRegistry(ctx, sensor.API, station.StationModel)

	err = rtkStation.Start(ctx)
	defer rtkStation.Close(ctx)
	if err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
