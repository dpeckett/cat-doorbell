// SPDX-License-Identifier: AGPL-3.0-or-later
/*
 * Copyright (C) 2024 Damian Peckett <damian@pecke.tt>.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dpeckett/cat-doorbell/internal/assets"
	"github.com/dpeckett/cat-doorbell/internal/constants"
	"github.com/dpeckett/cat-doorbell/internal/util"
	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/urfave/cli/v2"
)

const (
	detectionTimeout = 5 * time.Minute
	mqttTopic        = "bluetooth/devices"
)

func main() {
	persistentFlags := []cli.Flag{
		&cli.GenericFlag{
			Name:  "log-level",
			Usage: "Set the log verbosity level",
			Value: util.FromSlogLevel(slog.LevelInfo),
		},
	}

	initLogger := func(c *cli.Context) error {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: (*slog.Level)(c.Generic("log-level").(*util.LevelFlag)),
		})))

		return nil
	}

	app := &cli.App{
		Name:    "cat-doorbell",
		Usage:   "Receive a notification when the cat wants to come inside",
		Version: constants.Version,
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:     "address",
				Aliases:  []string{"a"},
				Usage:    "The address of the MQTT broker",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "username",
				Aliases:  []string{"u"},
				Usage:    "MQTT username for authentication",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "password",
				Aliases:  []string{"p"},
				Usage:    "MQTT password for authentication",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "target-mac",
				Aliases:  []string{"m"},
				Usage:    "The MAC address of the device to listen for",
				Required: true,
			},
		}, persistentFlags...),
		Before: initLogger,
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)

			var err error
			systray.Run(func() {
				var iconData []byte
				iconData, err = assets.ReadFile("cat-icon.png")
				if err != nil {
					systray.Quit()
					return
				}

				systray.SetIcon(iconData)
				systray.SetTooltip("Doorbell")

				mQuit := systray.AddMenuItem("Quit", "Quit the application")

				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

				go func() {
					select {
					case <-mQuit.ClickedCh:
						systray.Quit()
					case <-sig:
						systray.Quit()
					}
				}()

				go func() {
					defer systray.Quit()

					err = run(ctx, c.String("address"), c.String("username"), c.String("password"), c.String("target-mac"))
				}()
			}, cancel)

			return err
		},
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("Failed to run the application", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, address, username, password, targetMAC string) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}

	// Configure MQTT client
	opts := paho.NewClientOptions().
		AddBroker(address).
		SetClientID(hostname).
		SetUsername(username).
		SetPassword(password)

	opts.OnConnect = func(_ paho.Client) {
		slog.Info("Connected to MQTT broker", slog.String("address", address))
	}

	opts.OnConnectionLost = func(_ paho.Client, err error) {
		slog.Warn("Lost connection to MQTT broker", slog.Any("error", err))
	}

	client := paho.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}
	defer client.Disconnect(250)

	var lastDetectedMu sync.Mutex
	var lastDetected time.Time

	// Unpack the notification icon.
	tempDir, err := os.MkdirTemp("", "cat-doorbell")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	catIconPath := filepath.Join(tempDir, "cat-icon.png")
	if err := assets.Unpack("cat-icon.png", catIconPath); err != nil {
		return fmt.Errorf("failed to unpack cat icon: %w", err)
	}

	if token := client.Subscribe(mqttTopic, 0, func(client paho.Client, msg paho.Message) {
		mac := string(msg.Payload())

		slog.Debug("Received beacon from device", slog.String("mac", mac))

		if strings.EqualFold(mac, targetMAC) {
			lastDetectedMu.Lock()
			defer lastDetectedMu.Unlock()

			if time.Since(lastDetected) >= detectionTimeout {
				lastDetected = time.Now()

				slog.Info("Detected target device", slog.String("mac", mac))

				message := fmt.Sprintf("Device %s came into range", mac)
				if err := beeep.Notify("Doorbell", message, catIconPath); err != nil {
					slog.Warn("Failed to raise notification", slog.Any("error", err))
				}

				if err := playDoorbell(); err != nil {
					slog.Warn("Failed to play doorbell sound", slog.Any("error", err))
				}
			} else {
				slog.Debug("Ignoring beacon from device", slog.String("mac", mac))
			}
		}
	}); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to MQTT topic: %w", token.Error())
	}

	<-ctx.Done()

	return ctx.Err()
}

func playDoorbell() error {
	sr := beep.SampleRate(44100)
	if err := speaker.Init(sr, sr.N(time.Second/10)); err != nil {
		return fmt.Errorf("failed to initialize speaker: %w", err)
	}

	f, err := assets.Open("doorbell.mp3")
	if err != nil {
		return fmt.Errorf("failed to open embedded sound asset: %w", err)
	}
	defer f.Close()

	s, _, err := mp3.Decode(f)
	if err != nil {
		return fmt.Errorf("failed to decode MP3: %w", err)
	}

	speaker.Play(beep.Seq(s, beep.Callback(func() {
		_ = s.Close()
		speaker.Close()
	})))

	return nil
}
