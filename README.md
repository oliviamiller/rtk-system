# rtk-system

Provides functionaliy to set up a rtk base station to send a RTCM correction streams through serial or i2c.
The rtk-no-network components can be used to recieve the correction data and output locations with up to 1 cm accuracy.
This module is experimental.

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
[SparkFun GPS-RTK ZED-F9P] (https://www.sparkfun.com/products/16481) <br />
[Configuring a module in viam] (https://docs.viam.com/extend/modular-resources//#configure-your-module)

