# cat-doorbell

Receive a notification when the cat wants to come inside (Bluetooth Low Energy).

You'd think just keeping an ear out for meowing would work, but if the house is
closed up and the cat is trying to enter through a different room, it's very
easy to miss the sound. This project is a simple way to get a notification when
the cat is at the door.

## Build

### Debian

```shell
sudo apt install libasound2-dev libgtk-3-dev libayatana-appindicator3-dev
go build
```

### MacOS

```shell
brew install create-dmg
./build-darwin.sh
```

## Usage

To receive a notification when the MAC address `AA:BB:CC:DD:EE:FF` is detected
by the Bluetooth receiver, run the following command:

```shell
./cat-doorbell -a tcp://doorbell-receiver:1883 -u cat-doorbell -p mypassword -m AA:BB:CC:DD:EE:FF
```

### Debian System Tray

To run the program in the system tray on Debian, you can use the following:

```shell
sudo apt install gnome-shell-extension-appindicator
```

Log out and back in, then enable the extension:

```shell
gnome-extensions enable ubuntu-appindicators@ubuntu.com
```

## Bluetooth Receiver Setup

You'll need a machine to act as the Bluetooth receiver. I'm using an old intel
NUC running Debian bookworm. The receiver will run a MQTT broker, and a client
to listen for Bluetooth Low Energy device advertisements.

### Prerequisites

* Docker

#### Tools

```shell
sudo apt install bluez mosquitto-clients
```

### Configure Broker

```shell
mkdir -p config
cat > config/mosquitto.conf <<EOF
password_file /mosquitto/config/passwordfile
allow_anonymous false
listener 1883
EOF
docker run -it --rm -v $(pwd)/config:/mosquitto/config \
  eclipse-mosquitto:latest \
  mosquitto_passwd -c -b /mosquitto/config/passwordfile cat-doorbell mypassword
```

### Run Broker

```shell
docker run -d --name mosquitto_broker -p 1883:1883 \
  -v $(pwd)/config:/mosquitto/config \
  eclipse-mosquitto:latest
```

### Run Bluetooth Receiver

You can now listen for Bluetooth Low Energy devices and publish their MAC
addresses to the broker.

```shell
sudo hcitool lescan --passive --duplicates | awk '{print $1}' | \
  mosquitto_pub -h localhost -p 1883 -u cat-doorbell -P mypassword -t "bluetooth/devices" -l
```
