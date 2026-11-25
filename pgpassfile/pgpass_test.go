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

package pgpassfile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unescape(s string) string {
	s = strings.Replace(s, `\:`, `:`, -1)
	s = strings.Replace(s, `\\`, `\`, -1)
	return s
}

var passfile = [][]string{
	{"test1", "5432", "larrydb", "larry", "whatstheidea"},
	{"test1", "5432", "moedb", "moe", "imbecile"},
	{"test1", "5432", "curlydb", "curly", "nyuknyuknyuk"},
	{"test2", "5432", "*", "shemp", "heymoe"},
	{"test2", "5432", "*", "*", `test\\ing\:`},
	{"localhost", "*", "*", "*", "sesam"},
	{"test3", "*", "", "", "swordfish"}, // user will be filled later
}

func TestParsePassFile(t *testing.T) {
	buf := bytes.NewBufferString(`# A comment
	test1:5432:larrydb:larry:whatstheidea
	test1:5432:moedb:moe:imbecile
	test1:5432:curlydb:curly:nyuknyuknyuk
	test2:5432:*:shemp:heymoe
	test2:5432:*:*:test\\ing\:
	localhost:*:*:*:sesam
		`)

	passfile, err := ParsePassfile(buf)
	require.Nil(t, err)

	assert.Len(t, passfile.Entries, 6)

	assert.Equal(t, "whatstheidea", passfile.FindPassword("test1", "5432", "larrydb", "larry"))
	assert.Equal(t, "imbecile", passfile.FindPassword("test1", "5432", "moedb", "moe"))
	assert.Equal(t, `test\ing:`, passfile.FindPassword("test2", "5432", "something", "else"))
	assert.Equal(t, "sesam", passfile.FindPassword("localhost", "9999", "foo", "bare"))

	assert.Equal(t, "", passfile.FindPassword("wrong", "5432", "larrydb", "larry"))
	assert.Equal(t, "", passfile.FindPassword("test1", "wrong", "larrydb", "larry"))
	assert.Equal(t, "", passfile.FindPassword("test1", "5432", "wrong", "larry"))
	assert.Equal(t, "", passfile.FindPassword("test1", "5432", "larrydb", "wrong"))
}
