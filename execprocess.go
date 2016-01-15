// The execprocess feature lets SPJS run anything on the command line as a pass-thru
// scenario. Obviously there are security concerns here if somebody opens up their
// SPJS to the Internet, however if a user opens SPJS to the Internet they are
// exposing a lot of things, so we will trust that users implement their own
// layer of security at their firewall, rather than SPJS managing it.

package main

import (
	"runtime"
	"strings"
	//"fmt"
	"encoding/json"
	"log"
	"os/exec"
	"regexp"
)

type ExecCmd struct {
	ExecStatus string
	Id         string
	Cmd        string
	Args       []string
	Output     string
	//Stderr  string
}

func execRun(command string) {
	log.Printf("About to execute command:%s\n", command)

	// we have to remove the word "exec " from the front
	re, _ := regexp.Compile("^exec\\s+")
	cleanCmd := re.ReplaceAllString(command, "")

	// see if there's an id, and if so, yank it out
	// grab any word after id: and do case insensitive (?i)
	reId := regexp.MustCompile("(?i)^id:[a-zA-z0-9_\\-]+")
	id := reId.FindString(cleanCmd)
	if len(id) > 0 {
		// we found an id at the start of the exec command, use it
		cleanCmd = reId.ReplaceAllString(cleanCmd, "")
		id = regexp.MustCompile("^id:").ReplaceAllString(id, "")
	}

	// trim it
	cleanCmd = regexp.MustCompile("^\\s*").ReplaceAllString(cleanCmd, "")
	cleanCmd = regexp.MustCompile("\\s*$").ReplaceAllString(cleanCmd, "")

	// now we have to split off the first command and pass the rest as args
	cmdArr := strings.Split(cleanCmd, " ")
	cmd := cmdArr[0]
	argArr := cmdArr[1:]
	oscmd := exec.Command(cmd, argArr...)

	// will block here until results are done
	cmdOutput, err := oscmd.CombinedOutput()

	endProgress()

	if err != nil {
		log.Printf("Command finished with error: %v "+string(cmdOutput), err)
		//h.broadcastSys <- []byte("Could not program the board")
		//mapD := map[string]string{"ProgrammerStatus": "Error", "Msg": "Could not program the board. It is also possible your serial port is locked by another app and thus we can't grab it to use for programming. Make sure all other apps that may be trying to access this serial port are disconnected or exited.", "Output": string(cmdOutput)}
		mapD := ExecCmd{ExecStatus: "Error", Id: id, Cmd: cmd, Args: argArr, Output: string(cmdOutput) + err.Error()}
		mapB, _ := json.Marshal(mapD)
		h.broadcastSys <- mapB
	} else {
		log.Printf("Finished without error. Good stuff. stdout: " + string(cmdOutput))
		//h.broadcastSys <- []byte("Flash OK!")
		mapD := ExecCmd{ExecStatus: "Done", Id: id, Cmd: cmd, Args: argArr, Output: string(cmdOutput)}
		mapB, _ := json.Marshal(mapD)
		h.broadcastSys <- mapB
		// analyze stdin

	}

}

type ExecRuntime struct {
	ExecRuntimeStatus string
	OS                string
	Arch              string
	Goroot            string
	NumCpu            int
}

// Since SPJS runs on any OS, you will need to query to figure out
// what OS we're on so you know the style of commands to send
func execRuntime() {
	// create the struct and send data back
	info := ExecRuntime{"Done", runtime.GOOS, runtime.GOARCH, runtime.GOROOT(), runtime.NumCPU()}
	bm, err := json.Marshal(info)
	if err == nil {
		h.broadcastSys <- bm
	}
}
