# rtk-system

## Usage 
Build a binary nammed rtk-system with:

```
go build -o rtk-system
```

If you need to build a binary for a different target environment, use the [viam canon tool](https://github.com/viamrobotics/canon)

## Example Configuration
```
{
  "modules": [
    {
      "executable_path": "<path-to-binary",
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
        "rctm_i2c_addr": 66,
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
