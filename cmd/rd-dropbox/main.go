package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path"

	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/alecthomas/kingpin.v2"
)

type credentials struct {
	host     string
	user     string
	password string
	database string
}

type RowDropbox struct {
	id      int
	path    string
	logPath string
}

var (
	verbose = kingpin.Flag("verbose", "Be chatty when running").Short('v').Bool()
)

// Snarf all the rows in the DROPBOX table and return a slice of rowDropbox.
func getDropboxPaths(paths []RowDropbox) []RowDropbox {
	var rowCount int
	var thisRow RowDropbox

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

	// How many paths are we looking at here?
	row := db.QueryRow("SELECT count(*) FROM DROPBOXES")
	err = row.Scan(&rowCount)
	if err != nil {
		log.Fatal(err)
	}
	if *verbose {
		fmt.Printf("Found %d dropboxes\n", rowCount)
	}

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

	if *verbose {
		fmt.Printf("before returning: paths: %v\n", paths)
	}
	return paths
}

func main() {
	var paths []RowDropbox

	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.Version("0.0.1")
	kingpin.Parse()

	paths = getDropboxPaths(paths)

	for i := range paths {
		fmt.Printf("Dropbox ID: %d, Path:%s, Log Path:%s\n", paths[i].id, paths[i].path, paths[i].logPath)

		if pathInfo, err := os.Stat(path.Dir(paths[i].path)); os.IsNotExist(err) {
			fmt.Printf("%s does not seem to exist.\n", path.Dir(paths[i].path))
		} else {
			if *verbose {
				fmt.Printf("Dir %v, mode: %o\n", pathInfo.IsDir(), pathInfo.Mode())
			}
		}
	}
}
