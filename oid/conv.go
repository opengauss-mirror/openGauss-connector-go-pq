/*
 * 2022/4/15 Bin Liu <bin.liu@enmotech.com>
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

package oid

import (
	"strings"
)

func ConvertOidsToString(oids []Oid) string {
	var list []string
	for _, l := range oids {
		s, ok := TypeName[l]
		if !ok {
			s = ""
		}
		list = append(list, s)
	}
	return strings.Join(list, ",")
}
