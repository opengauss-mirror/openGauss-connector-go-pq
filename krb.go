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

package pq

// NewGSSFunc creates a GSS authentication provider, for use with
// RegisterGSSProvider.
type NewGSSFunc func() (GSS, error)

var newGss NewGSSFunc

// RegisterGSSProvider registers a GSS authentication provider. For example, if
// you need to use Kerberos to authenticate with your server, add this to your
// main package:
//
//	import "gitcode.com/opengauss/openGauss-connector-go-pq/auth/kerberos"
//
//	func init() {
//		pq.RegisterGSSProvider(func() (pq.GSS, error) { return kerberos.NewGSS() })
//	}
func RegisterGSSProvider(newGssArg NewGSSFunc) {
	newGss = newGssArg
}

// GSS provides GSSAPI authentication (e.g., Kerberos).
type GSS interface {
	GetInitToken(host string, service string) ([]byte, error)
	GetInitTokenFromSpn(spn string) ([]byte, error)
	Continue(inToken []byte) (done bool, outToken []byte, err error)
}
