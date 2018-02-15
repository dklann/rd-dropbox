// rd-dropbox ; check Rivendell dropbox paths and permissions to make sure they are reasonable.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/shirou/gopsutil/process"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/shirou/gopsutil/process"
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

// Snarf all the rows in the DROPBOX table and return a slice of rowDropbox.
func (p rowDropbox) getDropboxPaths() []rowDropbox {
	var rowCount int
	var thisRow rowDropbox
	var paths []rowDropbox

	stationName, err := os.Hostname()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting my host name: %s\n", err.Error())
	} else {
		if *verbose {
			fmt.Printf("\tOur station name: %s\n", stationName)
		}
	}

	db, err := sql.Open("mysql", *dbuser+":"+*dbpass+"@tcp("+*dbhost+")/"+*dbname)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// From https://github.com/golang/go/wiki/SQLInterface
	// "Note that Open does not directly open a database connection"
	if err = db.Ping(); err == nil {
		if *verbose {
			fmt.Println("\tpinged the database")
		}
	} else {
		log.Fatal(err)
	}

	// How many paths are we looking at here?
	row := db.QueryRow("SELECT count(*) FROM DROPBOXES WHERE station_name = '" + stationName + "'")
	err = row.Scan(&rowCount)
	if err != nil {
		log.Fatal(err)
	}
	if *verbose {
		fmt.Printf("\tFound %d dropboxes\n", rowCount)
	}

	rows, err := db.Query("SELECT id,path,log_path FROM DROPBOXES WHERE station_name = '" + stationName + "'")
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
		fmt.Printf("\tBefore returning: paths: %v\n", paths)
	}
	return paths
}

func main() {
	p := rowDropbox{}
	var paths []rowDropbox
	var restartPIDs []int32

	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.UsageTemplate(kingpin.CompactUsageTemplate).Version("0.0.1").Author("Broadcast Tool & Die, David Klann")
	kingpin.CommandLine.Help = "Check and, if necessary restart Rivendell dropbox tasks."
	kingpin.Parse()

	paths = p.getDropboxPaths()

	for i := range paths {
		// Check (and attempt to correct) the dropbox path spec.
		if pathInfo, err := os.Stat(path.Dir(paths[i].path)); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Path spec directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].path))
			err = os.MkdirAll(path.Dir(paths[i].path), 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "main: Unable to create path spec directory '%s' (%s).\n", path.Dir(paths[i].path), err.Error())
			} else {
				if *verbose {
					fmt.Printf("\tmain: Successfully created '%s'.\n", path.Dir(paths[i].path))
				}
			}
		} else if os.IsPermission(err) {
			fmt.Fprintf(os.Stderr, "Path spec directory '%s' is not readable. I'll try to fix it.\n", path.Dir(paths[i].path))
			err = os.Chmod(path.Dir(paths[i].path), 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Unable to change permissions on directory '%s' (%s). You're on your own.\n", path.Dir(paths[i].path), err.Error())
			} else {
				if *verbose {
					fmt.Printf("\tmain: Successfully set permissions on '%s'.\n", path.Dir(paths[i].path))
				}
			}
		} else {
			if *verbose {
				fmt.Printf("\tmain: path spec dir '%s': %v, mode: %o OK\n", path.Dir(paths[i].path), pathInfo.IsDir(), pathInfo.Mode())
			}
		}

		// Check (and attempt to correct) the dropbox LOG_PATH directory (we check the actual file below).
		if pathInfo, err := os.Stat(path.Dir(paths[i].logPath)); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Log directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].logPath))
			err = os.MkdirAll(path.Dir(paths[i].logPath), 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "main: Unable to create log path directory '%s' (%s).\n", path.Dir(paths[i].logPath), err.Error())
			} else {
				if *verbose {
					fmt.Printf("\tmain: Successfully created '%s'.\n", paths[i].logPath)
				}
			}
		} else if os.IsPermission(err) {
			fmt.Fprintf(os.Stderr, "Log path directory '%s' is not readable. I'll try to fix it.\n", path.Dir(paths[i].logPath))
			err = os.MkdirAll(path.Dir(paths[i].logPath), 0755)
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "Unhandled error: err: %v\n", err)
		} else {
			if *verbose {
				fmt.Printf("\tmain: log dir '%s': %v, mode: %o. OK.\n", path.Dir(paths[i].logPath), pathInfo.IsDir(), pathInfo.Mode())
			}
			// The LOG_PATH is accessible, make sure the log FILE is accessible and writable.
			if pathInfo, err = os.Stat(paths[i].logPath); os.IsPermission(err) {
				fmt.Fprintf(os.Stderr, "Could not access log file '%s' (err: %s). I will try to correct this...\n", paths[i].logPath, err.Error())
				if err = os.Chmod(path.Dir(paths[i].logPath), 0755); os.IsPermission(err) {
					fmt.Fprintf(os.Stderr, "Could not update permission on '%s' (%v). Please correct this situation.\n", path.Dir(paths[i].logPath), err)
				} else if err != nil {
					fmt.Fprintf(os.Stderr, "Unhandled error when trying to correct permission on '%s': %v", path.Dir(paths[i].logPath), err.Error())
				}
			} else {
				if *verbose {
					fmt.Printf("\tmain: log file exists '%s': mode: %v. OK.\n", paths[i].logPath, pathInfo.Mode())
				}

				// Use the process pkg to get a slice containing all the currently running processes.
				if processList, err := process.Processes(); err == nil {
					for p := range processList {
						if pName, _ := processList[p].Name(); pName == "rdimport" {
							// CmdlineSlice() returns a slice containing the command args,
							// we are looking for our current dropbox path spec in that slice.
							if pArgs, err := processList[p].CmdlineSlice(); err == nil {
								for a := range pArgs {
									if pArgs[a] == paths[i].path {
										if *verbose {
											fmt.Printf("\tmain: Found process ID %d for dropbox ID %d (%s)\n", processList[p].Pid, paths[i].id, path.Dir(paths[i].path))
										}
										paths[i].rdimportPID = processList[p].Pid
										paths[i].proc = *processList[p]
										restartPIDs = append(restartPIDs, processList[p].Pid)
										break
									}
								}
							} else {
								fmt.Fprintf(os.Stderr, "Unable to read command line args for process '%s' (PID: %d)\n", pName, processList[p].Pid)
							}
						}
					}
				} else {
					fmt.Fprintf(os.Stderr, "Trouble getting the current list of running processes: %v\n", err)
				}
				if paths[i].rdimportPID < 1 {
					fmt.Fprintf(os.Stderr, "Unable to find a running process for dropbox ID %d (%s)\n", paths[i].id, path.Dir(paths[i].path))
				}
			}
		}
	}

	// Completed checking the paths and running processes,
	// now restart rdcatchd(8) only if we are missing any PIDs.
	if len(restartPIDs) == len(paths) {
		fmt.Println("Yay! All Rivendell dropboxes are running.")
	} else {
		fmt.Fprintf(os.Stderr, "Missing one or more rdimport processes, attempting to stop all remaining process ...\n")
		// Kill the remaining instances of rdimport
		for p := range restartPIDs {
			for r := range paths {
				if paths[r].rdimportPID == restartPIDs[p] {
					if *verbose {
						fmt.Printf("\tkilling proccess for dropbox path %s ID: %d ...", paths[r].path, paths[r].proc.Pid)
					}
					if err := paths[r].proc.Kill(); err != nil {
						fmt.Fprintf(os.Stderr, "Error attempting to stop dropbox PID %d: %#v", paths[r].proc.Pid, err)
					} else {
						if *verbose {
							fmt.Println(" killed.")
						}
					}
				}
			}
		}
		// kill and restart rdcatchd(8)
		if processList, err := process.Processes(); err == nil {
			for p := range processList {
				if pName, _ := processList[p].Name(); pName == "rdcatchd" {
					if *verbose {
						fmt.Printf("\tkilling %s process ID %d ...", pName, processList[p].Pid)
					}
					if err := processList[p].Kill(); err != nil {
						fmt.Fprintf(os.Stderr, "Error attempting to stop dropbox manager service 'rdcatchd': %#v\n", err)
					} else {
						if *verbose {
							fmt.Println(" killed.")
						}
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
					if *verbose {
						fmt.Printf("\t%s was restarted: new process ID %d ...", pName, processList[p].Pid)
					}
				} else if err != nil {
					fmt.Fprintf(os.Stderr, "Error retrieving info about rdcatchd process (%v)", err)
				}
			}
		}
		if rdcatchdPIDfound {
			if *verbose {
				fmt.Println("rdcatchd seems to have been restarted for us. Moving along now ...")
			}
		} else {
			// Not found, so we need to restart it. First, make sure we can see the executable.
			// Note that rdcatchd puts itself into the background. Grrrrr...
			if rdcatchdPath, err := exec.LookPath("rdcatchd"); err != nil {
				log.Fatal("Cannot find executable 'rdcatchd' in $PATH")
			} else {
				command := exec.Command(rdcatchdPath)
				if err := command.Run(); err != nil {
					log.Fatalf("Oh No! Could not launch command '%s': %#v\n", rdcatchdPath, err)
				} else {
					if *verbose {
						fmt.Printf("\tsuccessfully (re)started rdcatchd\n")
					}
				}
			}
		}
	}
}
