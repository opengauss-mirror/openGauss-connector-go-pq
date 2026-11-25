// +build !windows

/*
 * Copyright (c) 2026 Huawei Technologies Co., Ltd.
 * All rights reserved.
 *
 * Licensed under the The MIT License (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://opensource.org/license/mit
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package pq

import (
	"os"
	"os/user"
	"path/filepath"
)

func defaultSettings() map[string]string {
	settings := make(map[string]string)

	settings[paramHost] = defaultHost()
	settings[paramPort] = "5432"

	// Default to the OS user name. Purposely ignoring err getting user name from
	// OS. The client application will simply have to specify the user in that
	// case (which they typically will be doing anyway).
	user, err := user.Current()
	if err == nil {
		settings[paramUser] = user.Username
		settings[paramPassFile] = filepath.Join(user.HomeDir, ".pgpass")
		settings[paramServiceFile] = filepath.Join(user.HomeDir, ".pg_service.conf")
	}

	settings[paramTargetSessionAttrs] = "any"

	settings[paramMinReadBufferSize] = "8192"
	settings[paramCpBufferSize] = "65536"
	// settings["client_encoding"] = "GBK"
	return settings
}

// defaultHost attempts to mimic libpq's default host. libpq uses the default unix socket location on *nix and localhost
// on Windows. The default socket location is compiled into libpq. Since conn does not have access to that default it
// checks the existence of common locations.
func defaultHost() string {
	candidatePaths := []string{
		"/var/run/postgresql", // Debian
		"/private/tmp",        // OSX - homebrew
		"/tmp",                // standard PostgreSQL
	}

	for _, path := range candidatePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "localhost"
}
