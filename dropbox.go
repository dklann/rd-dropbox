package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/dklann/mycnf"

	"github.com/davecgh/go-spew/spew"
	"github.com/shirou/gopsutil/process"
)

type rowDropbox struct {
	id          int
	path        string
	logPath     string
	rdimportPID int32
	proc        process.Process
}

// DropBoxer is an interface to the rowDropbox type.
type DropBoxer interface {
	getDropboxPaths() []rowDropbox
	removePathSpec() []rowDropbox
}

func newDropboxer() *rowDropbox {
	return &rowDropbox{
		id:          0,
		path:        "",
		logPath:     "",
		rdimportPID: 0,
		proc:        process.Process{},
	}
}

// Snarf all the rows in the DROPBOX table and return a slice of rowDropbox.
func (p *rowDropbox) getDropboxPaths(myconfig *string) (paths []rowDropbox, err error) {
	var rowCount int
	var thisRow rowDropbox

	// We are declaring these outside the conditionals so that we can use them
	// further on down the line in the method (it's a scope thing...).
	stationName, err := os.Hostname()
	if err != nil {
		log.Println("What? Who am I?", err)
		return nil, err
	}
	verbosePrint(fmt.Sprintf("getDropboxPaths: Our station name: %s", stationName))

	// Grab a new MyCnf object and set its values to those passed on the command line.
	mycnf := mycnf.NewMyCnf()
	if *dbhost != "" {
		mycnf.DbHost = *dbhost
	}
	if *dbname != "" {
		mycnf.DbName = *dbname
	}
	if *dbuser != "" {
		mycnf.DbUser = *dbuser
	}
	if *dbpass != "" {
		mycnf.DbPass = *dbpass
	}

	myCnf, err := mycnf.ReadMyCnf(myconfig, "client")
	if err != nil {
		return nil, err
	}
	if myCnf == "" {
		myCnf = *dbuser + ":" + *dbpass + "@tcp(" + *dbhost + ":3306)/" + *dbname // Use defaults if no .my.cnf config
	}
	verbosePrint(fmt.Sprintf("myCnf: %s", myCnf))
	db, err := sql.Open("mysql", myCnf)
	if err != nil {
		log.Printf("Error attempting to open database '%s' (%v).\n", *dbname, err)
		return nil, err
	}
	defer db.Close()

	// From https://github.com/golang/go/wiki/SQLInterface
	// "Note that Open does not directly open a database connection"
	if err = db.Ping(); err != nil {
		log.Printf("Error pinging the database (%v).\n", err)
		return nil, err
	}
	verbosePrint("getDropboxPaths: pinged the database")

	// How many paths are we looking at here?
	row := db.QueryRow("SELECT count(*) FROM DROPBOXES WHERE station_name = '" + stationName + "'")
	err = row.Scan(&rowCount)
	if err != nil {
		log.Printf("Error encountered determining the number of dropboxes from database (%v).\n", err)
		return nil, err
	}
	verbosePrint(fmt.Sprintf("getDropboxPaths: found %d dropboxes", rowCount))

	rows, err := db.Query("SELECT id,path,log_path FROM DROPBOXES WHERE station_name = '" + stationName + "'")
	if err != nil {
		log.Printf("Error encountered querying the database (%v).\n", err)
		return nil, err
	}
	for rows.Next() {
		if err := rows.Scan(&thisRow.id, &thisRow.path, &thisRow.logPath); err != nil {
			log.Printf("Error encountered retrieving row %d from the database (%v).\n", len(paths)+1, err)
			return nil, err
		}
		paths = append(paths, thisRow)
	}
	debugPrint(fmt.Sprintf("getDropboxPaths: before returning: paths: %s", spew.Sdump(paths)))

	return paths, nil
}

// Remove a path spec from the slice
func (p rowDropbox) removePathSpec(i int, paths []rowDropbox) (newpaths []rowDropbox) {
	verbosePrint(fmt.Sprintf("removePathSpec: removing dropbox ID %d ('%s') from the list of paths to consider for restarting dropboxes.", paths[i].id, paths[i].path))
	// Remove this item so we do not restart rdcatchd(8) for an invalid path spec.
	if i == len(paths)-1 {
		newpaths = append(paths[:i])
		debugPrint(fmt.Sprintf("removePathSpec: first pass thru paths[]: %s", spew.Sdump(paths)))
	} else {
		newpaths = append(paths[:i], paths[i+1:]...)
		debugPrint(fmt.Sprintf("removePathSpec: subsequent pass thru paths[]: %s", spew.Sdump(paths)))
	}
	debugPrint(fmt.Sprintf("removePathSpec: newpaths before returning: %v\n\n", newpaths))

	return newpaths
}
