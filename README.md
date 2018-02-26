# rd-dropbox

A Rivendell dropbox checker in Go.

This app queries the DROPBOXES table in the Rivendell database for the PATH and LOG_PATH columns. It then checks various aspects of those path specs and tries to ensure they are acceptable (permissions, viable pathnames, etc.). It attempts to restart rdcatchd(8) if necessary  (i.e., if any rdimport(1) processes are not running) after making what changes it can to the path specs.
