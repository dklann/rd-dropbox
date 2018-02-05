package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/alecthomas/kingpin.v2"
)

type credentials struct {
	host     string
	user     string
	password string
	database string
}

type rowDropbox struct {
	id      int
	path    string
	logPath string
}

// Snarf all the rows in the DROPBOX table and return a slice of rowDropbox.
func getDropboxPaths(db *sql.DB, verbose bool) []rowDropbox {
	var rowCount int
	var thisRow rowDropbox

	// How many paths are we looking at here?
	row := db.QueryRow("SELECT count(id) FROM DROPBOXES")
	err := row.Scan(&rowCount)
	if err != nil {
		log.Fatal(err)
	}
	if verbose {
		fmt.Printf("Found %d dropboxes\n", rowCount)
	}

	paths := make([]rowDropbox, rowCount, rowCount+1)

	rows, err := db.Query("SELECT id,path,log_path FROM DROPBOXES")
	if err != nil {
		log.Fatal(err)
	}

	for rows.Next() {
		if err := rows.Scan(&thisRow.id, &thisRow.path, &thisRow.logPath); err != nil {
			log.Fatal(err)
		}
		paths = append(paths, thisRow)
	}

	return paths
}

var (
	verbose = kingpin.Flag("verbose", "Be chatty when running").Short('v').Bool()
)

func main() {
	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.Version("0.0.1")
	kingpin.Parse()

	db, err := sql.Open("mysql", "rduser:8-XNmVk1WYHYgXEE@tcp(service)/Rivendell")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// From https://github.com/golang/go/wiki/SQLInterface
	// "Note that Open does not directly open a database connection"
	if err = db.Ping(); err == nil {
		if *verbose {
			fmt.Println("pinged the databass")
		}
	} else {
		log.Fatal(err)
	}

	paths := getDropboxPaths(db, *verbose)

	if *verbose {
		for i := range paths {
			fmt.Printf("%d %s %s\n", paths[i].id, paths[i].path, paths[i].logPath)
		}
	}
}
