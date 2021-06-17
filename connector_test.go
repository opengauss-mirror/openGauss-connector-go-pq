// +build go1.10

package pq

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	_ "github.com/jackc/pgx/v4/stdlib"
	"testing"
)

func TestNewConnector_WorksWithOpenDB(t *testing.T) {
	name := ""
	c, err := NewConnector(name)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(c)
	defer db.Close()
	// database/sql might not call our Open at all unless we do something with
	// the connection
	txn, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	txn.Rollback()
}

func TestNewConnector_Connect(t *testing.T) {
	name := ""
	c, err := NewConnector(name)
	if err != nil {
		t.Fatal(err)
	}
	db, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// database/sql might not call our Open at all unless we do something with
	// the connection
	txn, err := db.(driver.ConnBeginTx).BeginTx(context.Background(), driver.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	txn.Rollback()
}

func TestNewConnector_Driver(t *testing.T) {
	name := ""
	c, err := NewConnector(name)
	if err != nil {
		t.Fatal(err)
	}
	db, err := c.Driver().Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// database/sql might not call our Open at all unless we do something with
	// the connection
	txn, err := db.(driver.ConnBeginTx).BeginTx(context.Background(), driver.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	txn.Rollback()
}

func Test_Connect_DB(t *testing.T) {

	db, err := sql.Open("opengauss", "postgres://gaussdb:mtkOP@123@127.0.0.1:5435,127.0.0.1:5434/postgres?sslmode=disable&loggerLevel=debug")
	if err != nil {
		t.Error(err)
		return
	}
	err = db.Ping()
	if err != nil {
		t.Error(err)
		return
	}
	var v string
	err = db.QueryRow("select version()").Scan(&v)
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(v)
	err = db.QueryRow("SELECT pg_is_in_recovery()").Scan(&v)
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(v)
}
