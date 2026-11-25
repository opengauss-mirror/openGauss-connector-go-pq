/*
 * Copyright (c) 2011-2013, 'pq' Contributors
 * Portions Copyright (C) 2011 Blake Mizerany
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

// Package pq is a pure Go openGauss driver for the database/sql package.

package pq

import (
	"path/filepath"
	"syscall"
)

// Perform Windows user name lookup identically to libpq.
//
// The PostgreSQL code makes use of the legacy Win32 function
// GetUserName, and that function has not been imported into stock Go.
// GetUserNameEx is available though, the difference being that a
// wider range of names are available.  To get the output to be the
// same as GetUserName, only the base (or last) component of the
// result is returned.
func userCurrent() (string, error) {
	pw_name := make([]uint16, 128)
	pwname_size := uint32(len(pw_name)) - 1
	err := syscall.GetUserNameEx(syscall.NameSamCompatible, &pw_name[0], &pwname_size)
	if err != nil {
		return "", ErrCouldNotDetectUsername
	}
	s := syscall.UTF16ToString(pw_name)
	u := filepath.Base(s)
	return u, nil
}
