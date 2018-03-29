// rd-dropbox ; check Rivendell dropbox paths and permissions to make sure they are reasonable.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/shirou/gopsutil/process"
	"gopkg.in/alecthomas/kingpin.v2"
)

var d DropBoxer

var (
	myconfig = kingpin.Flag("myconfig", "The full path to a .my.cnf configuration file").Short('m').Default(os.Getenv("HOME") + "/.my.cnf").String()
	dbhost   = kingpin.Flag("dbhost", "The name or IP address of the database host").Short('d').String()
	dbuser   = kingpin.Flag("dbuser", "The name of the database user").Short('u').Default("rduser").String()
	dbpass   = kingpin.Flag("dbpass", "The password for the database user").Short('p').String()
	dbname   = kingpin.Flag("dbname", "The name of the database.").Short('n').Default("Rivendell").String()
	verbose  = kingpin.Flag("verbose", "Be chatty when running").Short('v').Bool()
	debug    = kingpin.Flag("debug", "Be very verbose about what is going on (implies -v).").Short('D').Bool()
)

const appVersion = "0.1.0"

// Add a bit of "template" around verbose print statements.
func verbosePrint(message string) {
	if *verbose {
		fmt.Println("\t" + message)
	}
	return
}

// Add a bit of "template" around verbose print statements.
func debugPrint(message string) {
	if *debug {
		fmt.Println("\t[D]: " + message)
	}
	return
}

func main() {
	p := newDropboxer()
	var paths []rowDropbox
	var returnError error
	var restartPIDs []int32
	var processList []*process.Process
	errorCount := 0

	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.UsageTemplate(kingpin.CompactUsageTemplate).Version(appVersion).Author("Broadcast Tool & Die, David Klann")
	kingpin.CommandLine.Help = "Check and, if necessary restart Rivendell dropbox tasks."
	kingpin.Parse()
	if *debug {
		*verbose = true
	}

	if paths, returnError = p.getDropboxPaths(myconfig); returnError != nil {
		log.Fatalf("Error getting dropbox paths from the database (%v).\n", returnError)
	}
	debugPrint(fmt.Sprintf("main: found %d elements in paths.\n", len(paths)))

	// Per https://stackoverflow.com/questions/28699485/remove-elements-in-slice
	// (the **Alternative** section), loop downward through paths[] so we do not have to
	// modify `i` when we remove an element from paths[]. The first pass through the loop
	// we simply copy paths[] to itself, leaving off the last element. On subsequent passes,
	// we need to copy the first elements, leave off this one, and then copy all the remaining
	// elements.
	for i := len(paths) - 1; i >= 0; i-- {
		var validPath = regexp.MustCompile(`^(/+\w+)+`)
		debugPrint(fmt.Sprintf("main: looking at dropbox ID %d - %+v'.", paths[i].id, paths[i]))
		if !validPath.MatchString(paths[i].path) {
			returnError = errors.New("empty or invalid file path")
			log.Printf("Error: Dropbox path spec '%s' (dropbox ID %d) is invalid (%v). Correct this in order to have a properly working dropbox.\n", paths[i].path, paths[i].id, returnError)
			errorCount++
		}
		// Check (and attempt to correct) the dropbox path spec.
		// NB: path.Dir() returns "." for the path if it is empty.
		if pathInfo, err := os.Stat(path.Dir(paths[i].path)); os.IsNotExist(err) {
			log.Printf("Path spec directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].path))
			err = os.MkdirAll(path.Dir(paths[i].path), 0755)
			if err != nil {
				log.Fatalf("main: Unable to create path spec directory '%s' (%s).\n", path.Dir(paths[i].path), err.Error())
			}
			verbosePrint(fmt.Sprintf("main: Successfully created '%s'.", path.Dir(paths[i].path)))
		} else if os.IsPermission(err) {
			log.Printf("Path spec directory '%s' is not readable. I'll try to fix it.\n", path.Dir(paths[i].path))
			err = os.Chmod(path.Dir(paths[i].path), 0755)
			if err != nil {
				log.Printf("Unable to change permissions on directory '%s' (%v). You're on your own.\n", path.Dir(paths[i].path), err)
				errorCount++
			}
			verbosePrint(fmt.Sprintf("main: Successfully set permissions on '%s'.", path.Dir(paths[i].path)))
		} else {
			verbosePrint(fmt.Sprintf("main: path spec dir '%s', mode: %v.", path.Dir(paths[i].path), pathInfo.Mode()))
			// Attempt to open/create a new file in the directory.
			// But just because *we* cannot _write_ to it does not mean the dropbox process is not running!?
			// All the dropbox process needs to do is read files in that directory.
			if !(path.Dir(paths[i].path) == ".") {
				if testFile, err := os.OpenFile(path.Dir(paths[i].path)+"/test-file", os.O_RDWR|os.O_CREATE, 0644); err != nil {
					log.Printf("Warning: Unable to create a new file in '%s' for dropbox ID %d (%v). Please correct this directory's ownership and/or permissions.\n", path.Dir(paths[i].path), paths[i].id, err)
					errorCount++
					// This is just a warning, NOT an error.
				} else {
					verbosePrint(fmt.Sprintf("main: path '%s' (dropbox ID %d) is writable.", path.Dir(paths[i].path), paths[i].id))
					testFile.Close()
					os.Remove(path.Dir(paths[i].path) + "/test-file")
				}
			} else {
				debugPrint("main: path spec should not be blank. How did we get here?")
			}
		}

		// Check (and attempt to correct) the dropbox LOG_PATH directory (we check the actual file below).
		if !validPath.MatchString(paths[i].logPath) {
			returnError = errors.New("empty or invalid file path")
			log.Printf("Error: Dropbox Log Path spec '%s' for dropbox ID %d is invalid (%v). Correct this in order to log activity for this dropbox.\n", paths[i].logPath, paths[i].id, returnError)
			errorCount++
			// Remove this entry in the paths slice because the dropbox is unlikely to be running.
			paths = p.removePathSpec(i, paths)
		} else {
			if pathInfo, err := os.Stat(path.Dir(paths[i].logPath)); os.IsNotExist(err) {
				log.Printf("Log directory '%s' does not seem to exist. I'll try to create it.\n", path.Dir(paths[i].logPath))
				err = os.MkdirAll(path.Dir(paths[i].logPath), 0755)
				if err != nil {
					log.Printf("Unable to create log path directory '%s' (%v).\n", path.Dir(paths[i].logPath), err)
					errorCount++
					// Remove this entry in the paths slice because the dropbox is unlikely to be running.
					paths = p.removePathSpec(i, paths)
				} else {
					verbosePrint(fmt.Sprintf("main: Successfully created '%s'.", path.Dir(paths[i].logPath)))
				}
			} else if os.IsPermission(err) {
				log.Printf("Log path directory '%s' is not readable (%v). You will need to fix it.\n", path.Dir(paths[i].logPath), err)
				errorCount++
				// Remove this entry from the slice because the dropbox is unlikely to be running.
				paths = p.removePathSpec(i, paths)
			} else if err != nil {
				log.Printf("Unexpected error on stat(2) of '%s': '%v'.\n", path.Dir(paths[i].logPath), err)
				errorCount++
				// Remove this entry in the paths slice because the dropbox is unlikely to be running.
				paths = p.removePathSpec(i, paths)
			} else {
				debugPrint(fmt.Sprintf("main: log dir '%s', mode: %v.", path.Dir(paths[i].logPath), pathInfo.Mode()))
				// The LOG_PATH is accessible, make sure the log FILE is accessible and writable.
				if pathInfo, err = os.Stat(paths[i].logPath); err != nil {
					log.Printf("Error: Could not access log file '%s' (%v). Is rdcatchd(8) running?\n", paths[i].logPath, err)
					errorCount++
					// Remove this entry from the slice because the dropbox is unlikely to be running.
					paths = p.removePathSpec(i, paths)
				} else if os.IsPermission(err) {
					log.Printf("Warning: Could not access log file '%s' (%v). I will try to correct this...\n", paths[i].logPath, err)
					if err = os.Chmod(path.Dir(paths[i].logPath), 0755); os.IsPermission(err) {
						log.Printf("Could not update permission on directory '%s' (%v). Please correct this situation.\n", path.Dir(paths[i].logPath), err)
						errorCount++
						// Remove this entry from the slice because the dropbox is unlikely to be running.
						paths = p.removePathSpec(i, paths)
					} else if err != nil {
						log.Printf("Unexpected error when trying to correct permission on '%s' (%v).", path.Dir(paths[i].logPath), err)
						errorCount++
						// Remove this entry from the slice because the dropbox is unlikely to be running.
						paths = p.removePathSpec(i, paths)
					}
				} else { // TODO: probably other errors to check for here.
					// We have permission to stat the file, but do we have permission to write to it?
					debugPrint(fmt.Sprintf("main: log file exists '%s': mode: %v.", paths[i].logPath, pathInfo.Mode()))
					if logPath, err := os.OpenFile(paths[i].logPath, os.O_RDWR, 0); err != nil {
						log.Printf("Error: Unable to open dropbox log file for dropbox ID %d (%v). Please correct this file's ownership and/or permissions.\n", paths[i].id, err)
						errorCount++
						// Remove this entry from the slice because the dropbox is unlikely to be running.
						paths = p.removePathSpec(i, paths)
					} else {
						verbosePrint(fmt.Sprintf("main: log_path '%s' (dropbox ID %d) is writable.", paths[i].logPath, paths[i].id))
						logPath.Close()
					}
				}
			}
		}
	}

	// Use the process pkg to get a slice containing all the currently running processes. We'll use this list to
	// determine if we need to restart rdcatchd(8)
	if processList, returnError = process.Processes(); returnError != nil {
		log.Fatalf("Trouble getting the current list of running processes (%v).\n", returnError)
	}
	// Iterate over paths and processes to match them up.
	// We'll have to restart rdimport(8) if any path is missing a running process
	for i := range paths {
		for p := range processList {
			if pName, _ := processList[p].Name(); pName == "rdimport" {
				// CmdlineSlice() returns a slice containing the command args;
				// we are looking for our current dropbox path spec in that slice.
				if pArgs, err := processList[p].CmdlineSlice(); err != nil {
					log.Printf("Unable to read command line args for process '%s' (PID: %d) (%v)\n", pName, processList[p].Pid, err)
					errorCount++
				} else {
					for a := range pArgs {
						if pArgs[a] == paths[i].path {
							verbosePrint(fmt.Sprintf("main: Found process ID %d for dropbox ID %d (%s)", processList[p].Pid, paths[i].id, paths[i].path))
							paths[i].rdimportPID = processList[p].Pid
							paths[i].proc = *processList[p]
							restartPIDs = append(restartPIDs, processList[p].Pid)
							break
						}
					}
				}
			}
		}
		if paths[i].rdimportPID < 1 {
			log.Printf("Unable to find a running process for dropbox ID %d (%s)\n", paths[i].id, path.Dir(paths[i].path))
		}
	}

	// Completed checking the paths and running processes,
	// now restart rdcatchd(8) only if we are missing any PIDs.
	if len(restartPIDs) != len(paths) {
		debugPrint(fmt.Sprintf("main: restartPIDs: %v, paths: %v", restartPIDs, paths))
		log.Printf("Missing one or more rdimport processes, attempting to stop all remaining process ...\n")
		// Kill the remaining instances of rdimport
		for p := range restartPIDs {
			for r := range paths {
				if paths[r].rdimportPID == restartPIDs[p] {
					verbosePrint(fmt.Sprintf("main: killing dropbox proccess for dropbox path '%s' ID: %d ...", paths[r].path, paths[r].proc.Pid))
					if err := paths[r].proc.Kill(); err != nil {
						log.Printf("Error attempting to stop dropbox PID %d (%v).\n", paths[r].proc.Pid, err)
						errorCount++
					}
				}
			}
		}
		// kill and restart rdcatchd(8)
		if processList, err := process.Processes(); err == nil {
			for p := range processList {
				if pName, _ := processList[p].Name(); pName == "rdcatchd" {
					verbosePrint(fmt.Sprintf("main: killing '%s' process ID %d ...", pName, processList[p].Pid))
					if err := processList[p].Kill(); err != nil {
						log.Printf("Error attempting to stop dropbox manager service 'rdcatchd' (%v)\n", err)
						errorCount++
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
					verbosePrint(fmt.Sprintf("main: %s was restarted: new process ID %d ...", pName, processList[p].Pid))
				} else if err != nil {
					log.Printf("Error retrieving info about rdcatchd process (%v).\n", err)
					errorCount++
				}
			}
		}
		if rdcatchdPIDfound {
			verbosePrint("main: rdcatchd seems to have been restarted for us. Moving along now ...")
		} else {
			// Running process not found, so we need to restart it. First, make sure we
			// can see the executable. Note that rdcatchd puts itself into the background. Grrrrr...
			if rdcatchdPath, err := exec.LookPath("rdcatchd"); err != nil {
				log.Fatalf("Fatal: Cannot find executable 'rdcatchd' in $PATH (%v). I quit.\n", err)
			} else {
				command := exec.Command(rdcatchdPath)
				if err := command.Run(); err != nil {
					log.Printf("Oh No! Could not launch command '%s' (%v).\n", rdcatchdPath, err)
					errorCount++
				} else {
					verbosePrint("main: successfully (re)started rdcatchd")
				}
			}
		}
	}
	if errorCount > 0 {
		log.Fatalf("Encountered %d errors. Please fix them and try again.\n", errorCount)
	}
}
