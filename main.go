// package main is a module with correction-station component
package main

import (
	"context"
	stationi2c "rtksystem/correction-station-i2c"
	serialstation "rtksystem/correction-station-serial"

	gpsrtki2cnonetwork "rtksystem/gps-rtk-i2c-no-network"
	gpsrtkserialnonetwork "rtksystem/gps-rtk-serial-no-network"

	"github.com/edaniels/golog"
	"go.viam.com/rdk/components/movementsensor"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/utils"
)

func main() {
	utils.ContextualMain(mainWithArgs, golog.NewDevelopmentLogger("rtk-system"))
}

func mainWithArgs(ctx context.Context, args []string, logger golog.Logger) error {
	rtkSystem, err := module.NewModuleFromArgs(ctx, logger)

	if err != nil {
		return err
	}
	rtkSystem.AddModelFromRegistry(ctx, sensor.API, serialstation.Model)
	rtkSystem.AddModelFromRegistry(ctx, sensor.API, stationi2c.Model)
	rtkSystem.AddModelFromRegistry(ctx, movementsensor.API, gpsrtkserialnonetwork.Model)
	rtkSystem.AddModelFromRegistry(ctx, movementsensor.API, gpsrtki2cnonetwork.Model)

	err = rtkSystem.Start(ctx)
	defer rtkSystem.Close(ctx)
	if err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}
