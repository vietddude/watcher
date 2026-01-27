package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	connStr := "postgres://watcher:watcher123@localhost:5432/watcher?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	content, err := os.ReadFile("scripts/add_wallets.sql")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec(string(content))
	if err != nil {
		panic(err)
	}

	fmt.Println("Successfully reset wallets from scripts/add_wallets.sql")
}
