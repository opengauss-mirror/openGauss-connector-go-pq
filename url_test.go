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

import (
	"testing"
)

func TestSimpleParseURL(t *testing.T) {
	expected := "host='hostname.remote'"
	str, err := ParseURL("postgres://hostname.remote")
	if err != nil {
		t.Fatal(err)
	}

	if str != expected {
		t.Fatalf("unexpected result from ParseURL:\n+ %v\n- %v", str, expected)
	}
}

func TestIPv6LoopbackParseURL(t *testing.T) {
	expected := "host='::1' port='1234'"
	str, err := ParseURL("postgres://[::1]:1234")
	if err != nil {
		t.Fatal(err)
	}

	if str != expected {
		t.Fatalf("unexpected result from ParseURL:\n+ %v\n- %v", str, expected)
	}
}

func TestFullParseURL(t *testing.T) {
	expected := `dbname='database' host='hostname.remote' password='top secret' port='1234' user='username'`
	str, err := ParseURL("postgres://username:top%20secret@hostname.remote:1234/database")
	if err != nil {
		t.Fatal(err)
	}

	if str != expected {
		t.Fatalf("unexpected result from ParseURL:\n+ %s\n- %s", str, expected)
	}
}

func TestInvalidProtocolParseURL(t *testing.T) {
	_, err := ParseURL("http://hostname.remote")
	switch err {
	case nil:
		t.Fatal("Expected an error from parsing invalid protocol")
	default:
		msg := "invalid connection protocol: http"
		if err.Error() != msg {
			t.Fatalf("Unexpected error message:\n+ %s\n- %s",
				err.Error(), msg)
		}
	}
}

func TestMinimalURL(t *testing.T) {
	cs, err := ParseURL("postgres://")
	if err != nil {
		t.Fatal(err)
	}

	if cs != "" {
		t.Fatalf("expected blank connection string, got: %q", cs)
	}
}
