# rtk-system

This module is experimental.

**Correction-Station-I2C and Correction-Station-Serial**  <br />
Provides functionality to set up a RTCM base station to send RTCM correction streams through serial or i2c communication
connected to a radio or Bluetooth transmitter. 
This can be used for when there is no network connection or there are no public or private NTRIP mount points nearby.
The base station should be set up with a clear view of the sky for the best fix. In the config, set your desired accuracy (meters)
and the time (seconds) to spend configuring the base station. The base station will be configured to output the correction messages and will begin surveying 
for it's position. It will continue surveying until both the minimum time and required accuracy are achieved, then will begin to output RTCM messages.



**GPS-RTK-I2C-No-Network and GPS-RTK-Serial-No-Network**  <br />
The rtk-no-network components are on the rovers and recieve the correction data from the station to output locations with up to 1 cm accuracy.
A radio or bluetooth module using one of the supported communication protocols can be used to communicate between the correction station and the rovers. 


## Usage 
Build a binary named rtk-system with:

```
go build -o rtk-system
```

If you need to build a binary for a different target environment, use the [viam canon tool](https://github.com/viamrobotics/canon)

## Example Configuration
```
{
  "modules": [
    {
      "executable_path": "<path-to-binary>",
      "name": "rtk-system"
    }
  ],
  "components": [
    {
      "model": "viam-labs:sensor:correction-station-i2c",
      "name": "station1",
      "type": "sensor",
      "attributes": {
        "required_accuracy": 5,
        "required_time_sec": 200,
        "i2c_addr": 66,
        "i2c_bus": 1
      },
      "depends_on": []
    },
     {
      "model": "viam-labs:sensor:correction-station-serial",
      "name": "station2",
      "type": "sensor",
      "attributes": {
        "required_accuracy": 5,
        "required_time_sec": 200,
        "serial_path": "<some-path>"
      },
      "depends_on": []
    },
    {
    "model": "viam-labs:movement-sensor:gps-rtk-i2c-no-network"
      "name": "rover1",
      "type": "movement_sensor",
      "attributes": {
        "rtcm_i2c_addr": 66,
        "i2c_bus": 1,
        "nmea_i2c_addr": 67
      },
      "depends_on": [],
    },
       {
      "model": "viam-labs:movementsensor:gps-rtk-serial-no-network",
      "name": "rover2",
      "type": "sensor",
      "attributes": {
        "serial_nmea_path": "<some-path>",
        "serial_correction_path": "<some-path>"
      },
      "depends_on": []
    }
  ]
}
```

## Relevant Links
[SparkFun GPS-RTK ZED-F9P](https://www.sparkfun.com/products/16481) <br />
[Configuring a module in viam](https://docs.viam.com/extend/modular-resources//#configure-your-module) <br /> 
[Setting up your own base station](https://learn.sparkfun.com/tutorials/setting-up-a-rover-base-rtk-system/all)

