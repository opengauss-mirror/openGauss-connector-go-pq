// Copyright © 2021 Bin Liu <bin.liu@enmotech.com>

package main

import (
	"database/sql"
	"fmt"
	_ "gitee.com/opengauss/openGauss-connector-go-pq"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

/*
需要有访问dbe_perf.global_instance_time的权限
CREATE USER dbuser_monitor with login monadmin PASSWORD 'Mon@1234';
grant usage on schema dbe_perf to dbuser_monitor;
grant select on dbe_perf.global_instance_time to dbuser_monitor;
CGO_ENABLED=0 GOOS=linux GOARCH=arm64
*/

var (
	dsnExample = `DSN="postgres://gaussdb:secret@foo,bar,baz/mydb?sslmode=disable"
DSN="postgres://gaussdb:secret@foo:1,bar:2,baz:3/mydb?sslmode=disable"
DSN="user=gaussdb password=secret host=foo,bar,baz port=5432 dbname=mydb sslmode=disable"
DSN="user=gaussdb password=secret host=foo,bar,baz port=5432,5432,5433 dbname=mydb sslmode=disable"`
)

func main() {
	// os.Setenv("DSN","postgres://dbuser_monitor:Mon@1234@127.0.0.1:1112,127.0.0.1:1111/postgres?sslmode=disable&loggerLevel=debug")
	connStr := os.Getenv("DSN")
	if connStr == "" {
		fmt.Println("please define the env DSN. example:\n" + dsnExample)
		return
	}
	fmt.Println("DNS:", connStr)
	db, err := sql.Open("opengauss", connStr)
	if err != nil {
		log.Fatal(err)
	}
	var (
		newTimer = time.NewTicker(1 * time.Second)
		doClose  = make(chan struct{}, 1)
	)

	go func() {
		for {
			select {
			case <-newTimer.C:
				if err := getNodeName(db); err != nil {
					fmt.Println(err)
				}
			case <-doClose:
				newTimer.Stop()
				return
			}
		}
	}()

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL) //nolint:staticcheck
	defer signal.Stop(sigChan)
	<-sigChan
	doClose <- struct{}{}

}

func getNodeName(db *sql.DB) error {

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Commit()
	var nodeName, sysdate string
	err = tx.QueryRow("select sysdate,node_name from dbe_perf.global_instance_time limit 1 ").
		Scan(&sysdate, &nodeName)
	if err != nil {
		return err
	}
	fmt.Println(sysdate, nodeName)
	return nil
}
