// rd-dropbox ; check Rivendell dropbox paths and permissions to make sure they are reasonable.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"time"

	"github.com/davecgh/go-spew/spew"
	_ "github.com/go-sql-driver/mysql"
	"github.com/shirou/gopsutil/process"
	"gopkg.in/alecthomas/kingpin.v2"
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
}

var d DropBoxer

type dbCredentials struct {
	databaseHost string
	databaseUser string
	databasePass string
	databaseName string
}

var (
	dbhost  = kingpin.Flag("dbhost", "The name or IP address of the database host").Short('d').Default("localhost").String()
	dbuser  = kingpin.Flag("dbuser", "The name of the database user").Short('u').Default("rduser").String()
	dbpass  = kingpin.Flag("dbpass", "The password for the database user").Short('p').Default("letmein").String()
	dbname  = kingpin.Flag("dbname", "The name of the database.").Short('n').Default("Rivendell").String()
	verbose = kingpin.Flag("verbose", "Be chatty when running").Short('v').Bool()
)

func verbosePrint(message string) {
	if *verbose {
		fmt.Println("\t" + message)
	}
	return
}

// Snarf all the rows in the DROPBOX table and return a slice of rowDropbox.
func (p rowDropbox) getDropboxPaths() ([]rowDropbox, error) {
	var rowCount int
	var thisRow rowDropbox
	var paths []rowDropbox
	var returnError error
	var errorMessage string

	// We are declaring these outside the conditionals so that we can use them
	// further on down the line in the method (it's a scope thing...).
	stationName, err := os.Hostname()
	if err == nil {
		verbosePrint(fmt.Sprintf("Our station name: %s", stationName))
		db, err := sql.Open("mysql", *dbuser+":"+*dbpass+"@tcp("+*dbhost+")/"+*dbname)
		if err == nil {
			defer db.Close()

			// From https://github.com/golang/go/wiki/SQLInterface
			// "Note that Open does not directly open a database connection"
			if err = db.Ping(); err == nil {
				verbosePrint("pinged the database")

				// How many paths are we looking at here?
				row := db.QueryRow("SELECT count(*) FROM DROPBOXES WHERE station_name = '" + stationName + "'")
				err = row.Scan(&rowCount)
				if err == nil {
					verbosePrint(fmt.Sprintf("Found %d dropboxes", rowCount))

					rows, err := db.Query("SELECT id,path,log_path FROM DROPBOXES WHERE station_name = '" + stationName + "'")
					if err == nil {
						for rows.Next() {
							if err := rows.Scan(&thisRow.id, &thisRow.path, &thisRow.logPath); err != nil {
								log.Printf("Error encountered retrieving row %d from the database (%v).\n", len(paths)+1, err)
								return nil, err
							}
							paths = append(paths, thisRow)
						}
						verbosePrint(fmt.Sprintf("Before returning: paths: %s", spew.Sdump(paths)))
					} else {
						errorMessage = fmt.Sprintf("Error encountered querying the database (%v).\n", err)
						returnError = err
					}
				} else {
					errorMessage = fmt.Sprintf("Error encountered determining the number of dropboxes from database (%v).\n", err)
					returnError = err
				}
			} else {
				errorMessage = fmt.Sprintf("Error pinging the database (%v).\n", err)
				returnError = err
			}
		} else {
			errorMessage = fmt.Sprintf("Error attempting to open database '%s' (%v).\n", *dbname, err)
			returnError = err
		}
	} else {
		errorMessage = "Error getting my host name\n"
		returnError = err
	}
	if returnError != nil {
		log.Print(errorMessage)
	}
	return paths, returnError
}

func main() {
	p := rowDropbox{}
	var paths []rowDropbox
	var returnError error
	var errorMessage string
	var restartPIDs []int32
	var processList []*process.Process
	var validPath = regexp.MustCompile(`^(/+\w+)+`)

	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.UsageTemplate(kingpin.CompactUsageTemplate).Version("0.0.2").Author("Broadcast Tool & Die, David Klann")
	kingpin.CommandLine.Help = "Check and, if necessary restart Rivendell dropbox tasks."
	kingpin.Parse()

	// Use the process pkg to get a slice containing all the currently running processes.
	if processList, returnError = process.Processes(); returnError == nil {
		if paths, returnError = p.getDropboxPaths(); returnError == nil {
			verbosePrint(fmt.Sprintf("Found %d elements in paths.\n", len(paths)))
			// Per https://stackoverflow.com/questions/28699485/remove-elements-in-slice
			// (the **Alternative** section), loop downward through paths[] so we do not have to
			// modify `i` when we remove an element from paths[]. The first pass through the loop
			// we simply copy paths[] to itself, leaving off the last element. On subsequent passes,
			// we need to copy the first elements, leave off this one, and then copy all the remaining
			// elements.
			for i := len(paths) - 1; i >= 0; i-- {
				if !validPath.MatchString(paths[i].path) {
					log.Printf("Error: Dropbox path spec '%s' (dropbox ID %d) is invalid. Correct this in order to have a properly working dropbox.\n", paths[i].path, paths[i].id)
					// Remove this item so we do not restart rdcatchd(8) for an invalid path spec.
					if i == len(paths)-1 {
						paths = append(paths[:i])
						verbosePrint(fmt.Sprintf("first pass thru paths[]: %s", spew.Sdump(paths)))
					} else {
						paths = append(paths[:i], paths[i+1:]...)
						verbosePrint(fmt.Sprintf("subsequent pass thru paths[]: %s", spew.Sdump(paths)))
					}
					continue
				}
				// Check (and attempt to correct) the dropbox path spec.
				if pathInfo, err := os.Stat(path.Dir(paths[i].path)); os.IsNotExist(err) {
					log.Printf("Path spec directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].path))
					err = os.MkdirAll(path.Dir(paths[i].path), 0755)
					if err != nil {
						log.Printf("main: Unable to create path spec directory '%s' (%s).\n", path.Dir(paths[i].path), err.Error())
						returnError = err
					} else {
						verbosePrint(fmt.Sprintf("main: Successfully created '%s'.", path.Dir(paths[i].path)))
					}
				} else if os.IsPermission(err) {
					log.Printf("Path spec directory '%s' is not readable. I'll try to fix it.\n", path.Dir(paths[i].path))
					err = os.Chmod(path.Dir(paths[i].path), 0755)
					if err != nil {
						log.Printf("Unable to change permissions on directory '%s' (%s). You're on your own.\n", path.Dir(paths[i].path), err.Error())
						returnError = err
					} else {
						verbosePrint(fmt.Sprintf("main: Successfully set permissions on '%s'.", path.Dir(paths[i].path)))
					}
				} else {
					verbosePrint(fmt.Sprintf("main: path spec dir '%s': %v, mode: %o OK", path.Dir(paths[i].path), pathInfo.IsDir(), pathInfo.Mode()))
				}

				// Check (and attempt to correct) the dropbox LOG_PATH directory (we check the actual file below).
				if !validPath.MatchString(paths[i].logPath) {
					log.Printf("Error: Dropbox Log Path spec '%s' for dropbox ID %d is invalid. Correct this in order to log activity for this dropbox.\n", paths[i].logPath, paths[i].id)
					// Unlike paths[i].path, there is nothing else to do if the logPath is invalid.
				} else {
					if pathInfo, err := os.Stat(path.Dir(paths[i].logPath)); os.IsNotExist(err) {
						log.Printf("Log directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].logPath))
						err = os.MkdirAll(path.Dir(paths[i].logPath), 0755)
						if err != nil {
							log.Printf("main: Unable to create log path directory '%s' (%s).\n", path.Dir(paths[i].logPath), err.Error())
							returnError = err
						} else {
							verbosePrint(fmt.Sprintf("main: Successfully created '%s'.", paths[i].logPath))
						}
					} else if os.IsPermission(err) {
						log.Printf("Log path directory '%s' is not readable. I'll try to fix it.\n", path.Dir(paths[i].logPath))
						err = os.MkdirAll(path.Dir(paths[i].logPath), 0755)
					} else if err != nil {
						log.Printf("Unexpected error on stat(2) of '%s': '%v'.\n", path.Dir(paths[i].logPath), err)
						returnError = err
					} else {
						verbosePrint(fmt.Sprintf("main: log dir '%s', mode: %v. OK.", path.Dir(paths[i].logPath), pathInfo.Mode()))
						// The LOG_PATH is accessible, make sure the log FILE is accessible and writable.
						if pathInfo, err = os.Stat(paths[i].logPath); os.IsPermission(err) {
							log.Printf("Could not access log file '%s' (%v). I will try to correct this...\n", paths[i].logPath, err)
							if err = os.Chmod(path.Dir(paths[i].logPath), 0755); os.IsPermission(err) {
								log.Printf("Could not update permission on directory '%s' (%v). Please correct this situation.\n", path.Dir(paths[i].logPath), err)
								returnError = err
							} else if err != nil {
								log.Printf("Unexpected error when trying to correct permission on '%s' (%v).", path.Dir(paths[i].logPath), err)
								returnError = err
							}
						} else {
							// We have permission to stat the file, but do we have permission to write to it?
							verbosePrint(fmt.Sprintf("main: log file exists '%s': mode: %v.", paths[i].logPath, pathInfo.Mode()))
							if logPath, err := os.OpenFile(paths[i].logPath, os.O_RDWR, 0); err != nil {
								log.Printf("Error: Unable to open dropbox log file for dropbox ID %d (%v). Please correct this file's ownership and/or permissions.\n", paths[i].id, err)
								returnError = err
							} else {
								verbosePrint(fmt.Sprintf("log_path '%s' (dropbox ID %d) is writable.", paths[i].logPath, paths[i].id))
								logPath.Close()
							}
						}
					}
				}

				// Use the process list we obtained up top to get a slice containing all the currently running processes.
				for p := range processList {
					if pName, _ := processList[p].Name(); pName == "rdimport" {
						// CmdlineSlice() returns a slice containing the command args,
						// we are looking for our current dropbox path spec in that slice.
						if pArgs, err := processList[p].CmdlineSlice(); err == nil {
							for a := range pArgs {
								if pArgs[a] == paths[i].path {
									verbosePrint(fmt.Sprintf("main: Found process ID %d for dropbox ID %d (%s)", processList[p].Pid, paths[i].id, paths[i].path))
									paths[i].rdimportPID = processList[p].Pid
									paths[i].proc = *processList[p]
									restartPIDs = append(restartPIDs, processList[p].Pid)
									break
								}
							}
						} else {
							log.Printf("Unable to read command line args for process '%s' (PID: %d) (%v)\n", pName, processList[p].Pid, err)
							returnError = err
						}
					}
				}
				if paths[i].rdimportPID < 1 {
					log.Printf("Unable to find a running process for dropbox ID %d (%s)\n", paths[i].id, path.Dir(paths[i].path))
				}
			}

			// Completed checking the paths and running processes,
			// now restart rdcatchd(8) only if we are missing any PIDs.
			if len(restartPIDs) == len(paths) {
				fmt.Println("Yay! All available Rivendell dropboxes are running. Note any 'invalid' path specs or log path specs.")
			} else {
				log.Printf("Missing one or more rdimport processes, attempting to stop all remaining process ...\n")
				// Kill the remaining instances of rdimport
				for p := range restartPIDs {
					for r := range paths {
						if paths[r].rdimportPID == restartPIDs[p] {
							verbosePrint(fmt.Sprintf("killing proccess for dropbox path %s ID: %d ...", paths[r].path, paths[r].proc.Pid))
							if err := paths[r].proc.Kill(); err != nil {
								log.Printf("Error attempting to stop dropbox PID %d (%v).\n", paths[r].proc.Pid, err)
								returnError = err
							}
						}
					}
				}
				// kill and restart rdcatchd(8)
				if processList, err := process.Processes(); err == nil {
					for p := range processList {
						if pName, _ := processList[p].Name(); pName == "rdcatchd" {
							verbosePrint(fmt.Sprintf("killing %s process ID %d ...", pName, processList[p].Pid))
							if err := processList[p].Kill(); err != nil {
								log.Printf("Error attempting to stop dropbox manager service 'rdcatchd' (%v)\n", err)
								returnError = err
							}
						}
					}
				}
				duration := time.Duration(4) * time.Second
				time.Sleep(duration) // pause long enough for rdcatchd to restart

				// See if rdcatchd(8) was restarted by system services (systemd(8), upstart*8), svc(8), etc.),
				// and restart it if not.
				rdcatchdPIDfound := false
				if processList, err := process.Processes(); err == nil {
					for p := range processList {
						if pName, err := processList[p].Name(); pName == "rdcatchd" {
							rdcatchdPIDfound = true
							verbosePrint(fmt.Sprintf("%s was restarted: new process ID %d ...", pName, processList[p].Pid))
						} else if err != nil {
							log.Printf("Error retrieving info about rdcatchd process (%v).\n", err)
							returnError = err
						}
					}
				}
				if rdcatchdPIDfound {
					verbosePrint("rdcatchd seems to have been restarted for us. Moving along now ...")
				} else {
					// Running process not found, so we need to restart it. First, make sure we
					// can see the executable. Note that rdcatchd puts itself into the background. Grrrrr...
					if rdcatchdPath, err := exec.LookPath("rdcatchd"); err != nil {
						log.Fatalf("Fatal: Cannot find executable 'rdcatchd' in $PATH (%v). I quit.\n", err)
					} else {
						command := exec.Command(rdcatchdPath)
						if err := command.Run(); err != nil {
							log.Printf("Oh No! Could not launch command '%s' (%v).\n", rdcatchdPath, err)
							returnError = err
						} else {
							verbosePrint("successfully (re)started rdcatchd")
						}
					}
				}
			}
		} else {
			errorMessage = fmt.Sprintf("Error getting dropbox paths from the database (%v).\n", returnError)
		}
	} else {
		errorMessage = fmt.Sprintf("Trouble getting the current list of running processes (%v).\n", returnError)
	}
	if returnError != nil {
		if errorMessage != "" {
			// We have already logged the message if errorMessage is empty.
			log.Fatal(errorMessage)
		}
	}
}
