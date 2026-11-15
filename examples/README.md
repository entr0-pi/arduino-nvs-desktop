# Example schemas and configurations

These files are starting points for managing the ESP32 GNSS firmware's NVS
(non-volatile storage) with the [ESP32 NVS Key-Pair Manager](../README.md).
NVS stores settings as key-value pairs grouped into named namespaces, and keeps
them across restarts and power loss.

## Why a schema or a previous configuration is required

Arduino-ESP32's [Preferences API](https://docs.espressif.com/projects/arduino-esp32/en/latest/api/preferences.html)
does not provide a method to discover the available namespaces or list their
keys. It requires the namespace and key names to be known in advance. The
desktop app's serial protocol also has no discovery command, so connecting the
ESP32 alone cannot populate the key-pair table.

Load either a **CSV schema** or a **previously saved JSON configuration** to tell
the app which namespaces, keys and value types to read:

- A CSV schema defines the keys and their types, with optional proposed values.
  Use it to inspect a device when you do not have a saved configuration.
- A JSON configuration contains both the key definitions and their saved
  values. It acts as a schema and supplies a draft to restore or reuse; no
  separate CSV is needed.

The table covers only the keys in the loaded file. Loading a file does not
write to the ESP32; imported values become draft values until you explicitly
review and confirm a write.

## Files in this folder

| File | Purpose |
| --- | --- |
| [template_full.csv](template_full.csv) | Combined schema for the `gnss`, `wifi` and `ntrip` namespaces. Use it to inspect all the settings covered by these templates. |
| [template_gnss.csv](template_gnss.csv) | Schema for the GNSS UART receive pin, transmit pin and baud rate. |
| [template_wifi.csv](template_wifi.csv) | Schema for Wi-Fi credentials, DHCP/static addressing and access-point settings. |
| [template_ntrip.csv](template_ntrip.csv) | Schema for the NTRIP client, including caster credentials, connection/retry settings and lockout state. |
| [nvs-config_gnss.json](nvs-config_gnss.json) | Version-2 configuration containing `gnss/rx_pin = 20`, `gnss/tx_pin = 21` and `gnss/baud = 115200`. Demonstrates a saved configuration that can be loaded without a CSV. |

The JSON pin assignments and baud rate are examples; match them to your board's
wiring and GNSS receiver before writing.

## Firmware namespaces

The [GNSS ESP32 firmware](https://github.com/entr0-pi/gnss#customization-core-value)
uses these configuration namespaces:

| Namespace | What it controls | Keys covered by the examples |
| --- | --- | --- |
| `gnss` | UART connection between the ESP32 and the GNSS receiver. | `rx_pin`, `tx_pin`, `baud` |
| `wifi` | Wi-Fi credentials, network addressing and the access-point setting. | `ssid`, `pass`, `dhcp`, `ip`, `gw`, `subnet`, `dns`, `accesspoint` |
| `ntrip` | NTRIP caster connection used to feed correction data to the GNSS receiver. | `enabled`, `host`, `port`, `mount`, `user`, `pass`, plus retry, timeout, buffering, GGA and lockout fields. |

The NTRIP templates include `lock_fails`, `lock_aband` and `lock_hash`, which
represent firmware-maintained lockout state. A key's presence in a schema does
not override firmware access restrictions or build-time locks.

For implementation details, see the firmware's
[GNSS settings](https://github.com/entr0-pi/gnss/blob/main/src/config_gnss.cpp),
[Wi-Fi settings](https://github.com/entr0-pi/gnss/blob/main/src/config_wifi.cpp)
and [NTRIP settings](https://github.com/entr0-pi/gnss/blob/main/src/config_ntrip.cpp).
Keep schemas consistent with the firmware version installed on your ESP32.

## CSV schema format

The header must be exactly `key,type,encoding,value`. A `namespace` row selects
the namespace for the following `data` rows:

```csv
key,type,encoding,value
gnss,namespace,,
rx_pin,data,i32,
tx_pin,data,i32,
baud,data,u32,
```

| CSV encoding | JSON type | Meaning |
| --- | --- | --- |
| `i32` | `int` | Signed 32-bit integer. |
| `u32` | `uint` | Unsigned 32-bit integer. |
| `u8` | `bool` | Boolean: CSV accepts `0` or `1`; JSON uses `false` or `true`. |
| `string` | `string` | Text, such as an SSID, hostname or password. |

An empty CSV `value` means no proposed value; it does not mean erase the key or
set an empty string. Non-empty values populate the draft. In particular,
`template_full.csv` and `template_wifi.csv` contain `dhcp,data,u8,0`, which
proposes disabling DHCP. Review that value and the static IP settings before
writing.

## JSON configuration format

The app uses version `2`. Each item in `entries` records its namespace (`ns`),
key, type and value, making the configuration self-contained:

```json
{
  "version": 2,
  "entries": [
    { "ns": "gnss", "key": "baud", "type": "uint", "value": 115200 }
  ]
}
```

Use **Save as JSON…** in the app to save a configuration for later reuse. A
saved file contains the exported keys and values, not a complete dump of every
namespace on the ESP32. JSON can explicitly represent an empty string as `""`.
Exported Wi-Fi and NTRIP passwords are stored in clear text; keep configurations
containing credentials private.

## Using an example

1. Connect the ESP32 from the **Connect** tab.
2. Open **Key Pair Management**, then use **Browse CSV…** or **Browse JSON…**
   to load one of these files.
3. Choose **Read device** to fetch current values for the loaded keys and compare
   them with any imported draft values.
4. Edit the draft, select the modified keys you want to apply, then choose
   **Write to ESP32** and confirm the changes.
5. Use **Save as JSON…** to save a configuration you can load again later.
