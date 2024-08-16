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

package v1alpha1

import (
	"fmt"
	"time"

	"github.com/dpeckett/cat-doorbell/internal/config/types"
)

const APIVersion = "catdoorbell.github.com/v1alpha1"

type Config struct {
	types.TypeMeta `yaml:",inline"`
	Broker         BrokerConfig `yaml:"broker"`
	// TargetMAC is the MAC address of the device to listen for.
	TargetMAC string `yaml:"targetMAC"`
	// DetectionTimeout is the duration to wait for the device to be detected.
	DetectionTimeout time.Duration `yaml:"detectionTimeout"`
}

type BrokerConfig struct {
	// Address is the address of the MQTT broker.
	Address string `yaml:"address"`
	// Username is the username for authenticating with the MQTT broker.
	Username string `yaml:"username"`
	// Password is the password for authenticating with the MQTT broker.
	Password string `yaml:"password"`
}

func (c *Config) GetAPIVersion() string {
	return APIVersion
}

func (c *Config) GetKind() string {
	return "Config"
}

func (c *Config) PopulateTypeMeta() {
	c.TypeMeta = types.TypeMeta{
		APIVersion: APIVersion,
		Kind:       "Config",
	}
}

func GetConfigByKind(kind string) (types.Config, error) {
	switch kind {
	case "Config":
		return &Config{}, nil
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}
