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

package kerberos

import (
	"net"
	"strings"
)

/*
 *  Find the A record associated with a hostname
 *  In general, hostnames supplied to the driver should be
 *  canonicalized because the KDC usually only has one
 *  principal and not one per potential alias of a host.
 */
func canonicalizeHostname(host string) (string, error) {
	canon := host

	name, err := net.LookupCNAME(host)
	if err != nil {
		return "", err
	}

	name = strings.TrimSuffix(name, ".")

	if name != "" {
		canon = name
	}

	return canon, nil
}
