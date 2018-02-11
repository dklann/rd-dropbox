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

type rowDropbx struct {
	id      int
	path    string
	logPath string
}

var (
	verbose = kingpin.Flag("verbose", "Be chatty when running").Short('v').Bool()
)

// Snarf all the rows in the DROPBOX table and return a slice of rowDropbox.
func getDropboxPaths(paths []rowDropbx) []rowDropbx {
	var rowCount int
	var thisRow rowDropbx

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
	var paths []rowDropbx

	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.Version("0.0.1")
	kingpin.Parse()

	paths = getDropboxPaths(paths)

	for i := range paths {
		// Check (and attempt to correct) the dropbox path spec.
		if pathInfo, err := os.Stat(path.Dir(paths[i].path)); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Path spec directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].path))
			err = os.MkdirAll(path.Dir(paths[i].path), 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "main: Unable to create path spec directory '%s' (%s).\n", path.Dir(paths[i].path), err.Error())
			}
		} else if os.IsPermission(err) {
			fmt.Fprintf(os.Stderr, "Path spec directory '%s' is not readable. I'll try to fix it.\n", path.Dir(paths[i].path))
			err = os.Chmod(path.Dir(paths[i].path), 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Unable to change permissions on directory '%s' (%s). You're on your own.\n", path.Dir(paths[i].path), err.Error())
			}
		} else {
			if *verbose {
				fmt.Printf("Path spec dir '%s': %v, mode: %o\n", path.Dir(paths[i].path), pathInfo.IsDir(), pathInfo.Mode())
			}
		}

		// Check (and attempt to correct) the dropbox logPath spec.
		if pathInfo, err := os.Stat(path.Dir(paths[i].logPath)); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Log directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].logPath))
			err = os.MkdirAll(path.Dir(paths[i].logPath), 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "main: Unable to create log path directory '%s' (%s).\n", path.Dir(paths[i].logPath), err.Error())
			}
		} else if os.IsPermission(err) {
			fmt.Fprintf(os.Stderr, "Log path directory '%s' is not readable. I'll try to fix it.\n", path.Dir(paths[i].logPath))
			err = os.MkdirAll(path.Dir(paths[i].logPath), 0755)
		} else {
			if *verbose {
				fmt.Printf("Log dir '%s': %v, mode: %o\n", path.Dir(paths[i].logPath), pathInfo.IsDir(), pathInfo.Mode())
			}
		}
	}
}
