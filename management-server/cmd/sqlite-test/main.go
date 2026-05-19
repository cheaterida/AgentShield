package main

import (
	"database/sql"
	"fmt"
	"os"
	_ "modernc.org/sqlite"
)

func main() {
	path := os.Getenv("DB_PATH")
	if path == "" {
		path = "/tmp/test.db"
	}
	fmt.Printf("Opening SQLite at: %s\n", path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		fmt.Printf("FAIL sql.Open: %v\n", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;")
	if err != nil {
		fmt.Printf("FAIL pragma: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK - SQLite file DB opened successfully")
	db.Close()
}
