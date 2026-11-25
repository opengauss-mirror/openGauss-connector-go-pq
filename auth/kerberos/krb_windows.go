//go:build windows
// +build windows

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
	"github.com/alexbrainman/sspi"
	"github.com/alexbrainman/sspi/negotiate"
)

// GSS implements the pq.GSS interface.
type GSS struct {
	creds *sspi.Credentials
	ctx   *negotiate.ClientContext
}

// NewGSS creates a new GSS provider.
func NewGSS() (*GSS, error) {
	g := &GSS{}
	err := g.init()

	if err != nil {
		return nil, err
	}

	return g, nil
}

func (g *GSS) init() error {
	creds, err := negotiate.AcquireCurrentUserCredentials()
	if err != nil {
		return err
	}

	g.creds = creds
	return nil
}

// GetInitToken implements the GSS interface.
func (g *GSS) GetInitToken(host string, service string) ([]byte, error) {

	host, err := canonicalizeHostname(host)
	if err != nil {
		return nil, err
	}

	spn := service + "/" + host

	return g.GetInitTokenFromSpn(spn)
}

// GetInitTokenFromSpn implements the GSS interface.
func (g *GSS) GetInitTokenFromSpn(spn string) ([]byte, error) {
	ctx, token, err := negotiate.NewClientContext(g.creds, spn)
	if err != nil {
		return nil, err
	}

	g.ctx = ctx

	return token, nil
}

// Continue implements the GSS interface.
func (g *GSS) Continue(inToken []byte) (done bool, outToken []byte, err error) {
	return g.ctx.Update(inToken)
}
