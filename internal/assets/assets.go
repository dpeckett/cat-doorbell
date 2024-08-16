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

package assets

import (
	"embed"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed files/*
var fsys embed.FS

// Open opens the file with the given name from the embedded filesystem.
func Open(name string) (fs.File, error) {
	return fsys.Open(filepath.Join("files", name))
}

// ReadFile reads the file with the given name from the embedded filesystem.
func ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(fsys, filepath.Join("files", name))
}

// Unpack extracts the file with the given name from the embedded filesystem to the given path.
func Unpack(name, path string) error {
	f, err := Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, f)
	return err
}
