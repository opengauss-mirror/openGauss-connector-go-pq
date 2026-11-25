// +build go1.10

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

package pq_test

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"gitcode.com/opengauss/openGauss-connector-go-pq"
)

func getTestDsn() (string, error) {
	dsn := os.Getenv("TEST_CONN_STRING")
	if dsn == "" {
		return "", fmt.Errorf("not define TEST_CONN_STRING env")
	}
	return dsn, nil
}

func ExampleConnectorWithNoticeHandler() {
	name, err := getTestDsn()
	if err != nil {
		log.Fatal(err)
	}
	// Base connector to wrap
	base, err := pq.NewConnector(name)
	if err != nil {
		log.Fatal(err)
	}
	// Wrap the connector to simply print out the message
	connector := pq.ConnectorWithNoticeHandler(base, func(notice *pq.Error) {
		fmt.Println("Notice sent: " + notice.Message)
	})
	db := sql.OpenDB(connector)
	defer db.Close()
	// Raise a notice
	sql := "DO language plpgsql $$ BEGIN RAISE NOTICE 'test notice'; END $$"
	if _, err := db.Exec(sql); err != nil {
		log.Fatal(err)
	}
	// Output:
	// Notice sent: test notice
}
