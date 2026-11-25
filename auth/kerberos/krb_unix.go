//go:build !windows
// +build !windows

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
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/spnego"
)

/*
 * UNIX Kerberos support, using jcmturner's pure-go
 * implementation
 */

// GSS implements the pq.GSS interface.
type GSS struct {
	cli *client.Client
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
	cfgPath, ok := os.LookupEnv("KRB5_CONFIG")
	if !ok {
		cfgPath = "/etc/krb5.conf"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	u, err := user.Current()
	if err != nil {
		return err
	}

	ccpath := "/tmp/krb5cc_" + u.Uid

	ccname := os.Getenv("KRB5CCNAME")
	if strings.HasPrefix(ccname, "FILE:") {
		ccpath = strings.SplitN(ccname, ":", 2)[1]
	}

	ccache, err := credentials.LoadCCache(ccpath)
	if err != nil {
		return err
	}

	cl, err := client.NewFromCCache(ccache, cfg, client.DisablePAFXFAST(true))
	if err != nil {
		return err
	}

	cl.Login()

	g.cli = cl

	return nil
}

// GetInitToken implements the GSS interface.
func (g *GSS) GetInitToken(host string, service string) ([]byte, error) {

	// Resolve the hostname down to an 'A' record, if required (usually, it is)
	if g.cli.Config.LibDefaults.DNSCanonicalizeHostname {
		var err error
		host, err = canonicalizeHostname(host)
		if err != nil {
			return nil, err
		}
	}

	spn := service + "/" + host

	return g.GetInitTokenFromSpn(spn)
}

// GetInitTokenFromSpn implements the GSS interface.
func (g *GSS) GetInitTokenFromSpn(spn string) ([]byte, error) {
	s := spnego.SPNEGOClient(g.cli, spn)

	st, err := s.InitSecContext()
	if err != nil {
		return nil, fmt.Errorf("kerberos error (InitSecContext): %s", err.Error())
	}

	b, err := st.Marshal()
	if err != nil {
		return nil, fmt.Errorf("kerberos error (Marshaling token): %s", err.Error())
	}

	return b, nil
}

// Continue implements the GSS interface.
func (g *GSS) Continue(inToken []byte) (done bool, outToken []byte, err error) {
	t := &spnego.SPNEGOToken{}
	err = t.Unmarshal(inToken)
	if err != nil {
		return true, nil, fmt.Errorf("kerberos error (Unmarshaling token): %s", err.Error())
	}

	state := t.NegTokenResp.State()
	if state != spnego.NegStateAcceptCompleted {
		return true, nil, fmt.Errorf("kerberos: expected state 'Completed' - got %d", state)
	}

	return true, nil, nil
}
