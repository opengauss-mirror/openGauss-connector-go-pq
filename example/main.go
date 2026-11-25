/*
 * Copyright © 2021 Bin Liu <bin.liu@enmotech.com>
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

package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "gitcode.com/opengauss/openGauss-connector-go-pq"
)

func main() {
	connStr := "host=127.0.0.1 port=5433 user=gaussdb password=mtkOP@128 dbname=postgres sslmode=prefer"
	db, err := sql.Open("opengauss", connStr)
	if err != nil {
		log.Fatal(err)
	}
	var date string
	err = db.QueryRow("select current_date ").Scan(&date)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(date)
}
