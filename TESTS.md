# Tests

## Running Tests

`go test` is used for testing. A running openGauss
server is required, with the ability to log in. The
database to connect to test with is "pqgotest," on
"localhost" but these can be overridden using [environment
variables](https://www.postgresql.org/docs/9.3/static/libpq-envars.html).

Example:

	PGHOST=/run/postgresql go test

## Benchmarks

A benchmark suite can be run as part of the tests:

	go test -bench .

## Example setup (Docker)

Run a openGauss container:

```
docker run --name openGauss_test \
--privileged=true -d -e GS_PASSWORD=Test@123 \
-p 5432:5432 \
enmotech/opengauss:latest
```

Run tests:

```
PGHOST=localhost PGPORT=5432 PGUSER=gaussdb PGPASSWORD=Test@123 PGSSLMODE=disable PGDATABASE=postgres go test
```
