#!/bin/sh
set -e

go build -o darwin/CatDoorbell.app/Contents/MacOS/CatDoorbell .

rm -f *.dmg
create-dmg --volname CatDoorbell --volicon darwin/CatDoorbell.app/Contents/Resources/CatDoorbell.icns CatDoorbell.dmg darwin