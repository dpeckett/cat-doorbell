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

	"github.com/adrg/xdg"
	"github.com/dpeckett/cat-doorbell/internal/assets"
	"github.com/dpeckett/cat-doorbell/internal/config"
	latestconfig "github.com/dpeckett/cat-doorbell/internal/config/v1alpha1"
	"github.com/dpeckett/cat-doorbell/internal/constants"
	"github.com/dpeckett/cat-doorbell/internal/util"
	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	slogmulti "github.com/samber/slog-multi"
	"github.com/urfave/cli/v2"
)

const (
	mqttTopic = "bluetooth/devices"
)

func main() {
	defaultConfigFilePath, err := xdg.ConfigFile("cat-doorbell/config.yaml")
	if err != nil {
		slog.Error("Failed to get default configuration file path", slog.Any("error", err))
		os.Exit(1)
	}

	defaultLogDir, err := xdg.StateFile("cat-doorbell/logs")
	if err != nil {
		slog.Error("Failed to get state directory", slog.Any("error", err))
		os.Exit(1)
	}

	persistentFlags := []cli.Flag{
		&cli.StringFlag{
			Name:    "config",
			Aliases: []string{"c"},
			Usage:   "Path to the configuration file",
			Value:   defaultConfigFilePath,
		},
		&cli.StringFlag{
			Name:  "log-dir",
			Usage: "Directory to store log files",
			Value: defaultLogDir,
		},
		&cli.GenericFlag{
			Name:  "log-level",
			Usage: "Set the log verbosity level",
			Value: util.FromSlogLevel(slog.LevelInfo),
		},
	}

	initLogger := func(c *cli.Context) error {
		logDir := c.String("log-dir")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			slog.Error("Failed to create state directory", slog.Any("error", err))
			os.Exit(1)
		}

		if err := removeOldLogs(logDir); err != nil {
			return fmt.Errorf("failed to remove old logs: %w", err)
		}

		logFileName := fmt.Sprintf("%d-%d-cat-doorbell.log", time.Now().Unix(), os.Getpid())
		logFile, err := os.OpenFile(filepath.Join(logDir, logFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		opts := &slog.HandlerOptions{
			Level: (*slog.Level)(c.Generic("log-level").(*util.LevelFlag)),
		}

		slog.SetDefault(slog.New(
			slogmulti.Fanout(
				slog.NewTextHandler(logFile, opts),
				slog.NewTextHandler(os.Stderr, opts),
			),
		))

		return nil
	}

	var conf *latestconfig.Config
	loadConfig := func(c *cli.Context) error {
		configFile, err := os.Open(c.String("config"))
		if err != nil {
			return fmt.Errorf("failed to open configuration file: %w", err)
		}
		defer configFile.Close()

		conf, err = config.FromYAML(configFile)
		if err != nil {
			return fmt.Errorf("failed to unmarshal configuration: %w", err)
		}

		return nil
	}

	app := &cli.App{
		Name:    "cat-doorbell",
		Usage:   "Receive a notification when the cat wants to come inside",
		Version: constants.Version,
		Flags:   persistentFlags,
		Before:  beforeAll(initLogger, loadConfig),
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

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

					err = run(ctx, conf)
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

func run(ctx context.Context, conf *latestconfig.Config) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}

	// Configure MQTT client
	opts := paho.NewClientOptions().
		AddBroker(conf.Broker.Address).
		SetClientID(fmt.Sprintf("%s-%d", hostname, os.Getpid())).
		SetUsername(conf.Broker.Username).
		SetPassword(conf.Broker.Password)

	opts.OnConnect = func(client paho.Client) {
		slog.Info("Connected to MQTT broker", slog.String("address", conf.Broker.Address))
	}

	opts.OnConnectionLost = func(_ paho.Client, err error) {
		slog.Warn("Lost connection to MQTT broker", slog.Any("error", err))
	}

	client := paho.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}
	defer client.Disconnect(250)

	// Initialize the speaker.
	sr := beep.SampleRate(44100)
	if err := speaker.Init(sr, sr.N(time.Second/10)); err != nil {
		return fmt.Errorf("failed to initialize speaker: %w", err)
	}

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

		if strings.EqualFold(mac, conf.TargetMAC) {
			lastDetectedMu.Lock()
			defer lastDetectedMu.Unlock()

			if time.Since(lastDetected) >= conf.DetectionTimeout {
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
	f, err := assets.Open("doorbell.mp3")
	if err != nil {
		return fmt.Errorf("failed to open embedded sound asset: %w", err)
	}

	s, _, err := mp3.Decode(f)
	if err != nil {
		return fmt.Errorf("failed to decode MP3: %w", err)
	}

	speaker.Play(beep.Seq(s, beep.Callback(func() {
		_ = f.Close()
		_ = s.Close()
	})))

	return nil
}

func removeOldLogs(logDir string) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return fmt.Errorf("failed to read logs directory: %w", err)
	}

	if len(entries) > 10 {
		for _, entry := range entries[:len(entries)-10] {
			if err := os.Remove(filepath.Join(logDir, entry.Name())); err != nil {
				return fmt.Errorf("failed to remove old log entry: %w", err)
			}
		}
	}

	return nil
}

func beforeAll(beforeFunc ...cli.BeforeFunc) cli.BeforeFunc {
	return func(c *cli.Context) error {
		for _, f := range beforeFunc {
			if err := f(c); err != nil {
				return err
			}
		}

		return nil
	}
}
