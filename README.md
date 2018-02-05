# rd-dropbox
A Rivendell dropbox checker in Go.

This app queries the DROPBOXES table in the Rivendell database for the PATH and LOG_PATH columns. It then checks various aspects of those path specs and tries to ensure they are acceptable. It restarts rdcatchd(8) if necessary after making changes to the path specs.
