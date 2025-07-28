package pq

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"sync"
	"strings"
)

type QueryResult struct {
	Rows []map[string]interface{}
	Err  error                    
}

func validateQuery(query string) {
	if strings.TrimSpace(query) == "" {
		log.Fatal("invalid SQL query: must be a non-empty string")
	}

	trimmedQuery := strings.TrimSpace(query)

	selectRegex := regexp.MustCompile(`(?i)^\s*(?:--.*\s*)*SELECT\s+`)
	if !selectRegex.MatchString(trimmedQuery) {
		log.Fatal("invalid SQL query: must be a SELECT statement")
	}

	noComments := regexp.MustCompile(`--.*$`).ReplaceAllString(trimmedQuery, "")
	statements := strings.Split(noComments, ";")
	validCount := 0
	for _, stmt := range statements {
		if strings.TrimSpace(stmt) != "" {
			validCount++
		}
	}
	if validCount != 1 {
		log.Fatalf("invalid SQL query: must contain exactly one query, found %d", validCount)
	}

	vectorOpRegex := regexp.MustCompile(`<->|<=>|<#>|<+>|<~>|<%>`)
	if !vectorOpRegex.MatchString(trimmedQuery) {
		log.Fatal("invalid SQL query: must contain vector operator <->, <=>, <+>, <~>, <%> or <#>")
	}
}


func buildSetSQL(scanParams map[string]interface{}) []string {
	if len(scanParams) == 0 {
		return nil
	}
	stmts := make([]string, 0, len(scanParams))
	for k, v := range scanParams {
		stmts = append(stmts, fmt.Sprintf("set %s=%v", k, v))
	}
	return stmts
}

func duplicate(conninfo string) (*sql.DB, error) {
	return sql.Open("opengauss", conninfo)
}

func parseRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		scanTargets := make([]interface{}, len(cols))
		for i := range scanTargets {
			var val interface{}
			scanTargets[i] = &val
		}

		if err := rows.Scan(scanTargets...); err != nil {
			return results, err
		}

		row := make(map[string]interface{})
		for i, col := range cols {
			val := *scanTargets[i].(*interface{})
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	return results, rows.Err()
}

func ExecuteMultiSearch(
	conninfo string,
	query string,
	args [][]interface{},
	scanParams map[string]interface{},
	threadCount int,
) ([][]map[string]interface{}, error) {
	validateQuery(query)

	if args == nil || len(args) == 0 {
        log.Fatal("args can not be empty")
    }

	if (threadCount <= 0) {
		log.Fatal("please confirm that the number of threads is greater than 0.")
	}

	var wg sync.WaitGroup
	resultChan := make(chan QueryResult, len(args)) 

	chunkSize := (len(args) + threadCount - 1) / threadCount
	for i := 0; i < threadCount; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(args) {
			end = len(args)
		}
		if start > end {
			break
		}

		chunk := args[start:end]
		if len(chunk) == 0 {
			continue
		}

		wg.Add(1)
		go func(chunk [][]interface{}) {
			defer wg.Done()
			newConn, err := duplicate(conninfo)
			if err != nil {
				resultChan <- QueryResult{Err: fmt.Errorf("failed to create connection: %v", err)}
				return
			}
			
			defer newConn.Close()
			setSQL := buildSetSQL(scanParams)
			if setSQL != nil {
			    for _, sql := range setSQL {
			        _, err = newConn.Exec(sql)
			        if err != nil {
			            resultChan <- QueryResult{Err: fmt.Errorf("failed to execute setSQL: %v (sql: %s)", err, sql)}
			            return
			        }
			    }
			}

			for _, arg := range chunk {
				rows, err := newConn.Query(query, arg...)
				if err != nil {
					resultChan <- QueryResult{Err: fmt.Errorf("query failed: %v", err)}
					continue
				}

				parsedRows, err := parseRows(rows)
				rows.Close() 
				if err != nil {
					resultChan <- QueryResult{Err: fmt.Errorf("parse failed: %v", err)}
					return
				}
				resultChan <- QueryResult{Rows: parsedRows}
			}
		}(chunk)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var allResults [][]map[string]interface{}
	for res := range resultChan {
		if res.Err != nil {
			return nil, res.Err
		}
		allResults = append(allResults, res.Rows) 
	}

	return allResults, nil
}
