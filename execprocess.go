// The execprocess feature lets SPJS run anything on the command line as a pass-thru
// scenario. Obviously there are security concerns here if somebody opens up their
// SPJS to the Internet, however if a user opens SPJS to the Internet they are
// exposing a lot of things, so we will trust that users implement their own
// layer of security at their firewall, rather than SPJS managing it.

package main

import (
	"bufio"
	//	"bytes"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
	//	"go/scanner"
	"runtime"

	"encoding/json"
	"log"
	"os"
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
		cleanCmd = strings.TrimPrefix(cleanCmd, " ")
		id = regexp.MustCompile("^id:").ReplaceAllString(id, "")
	}

	// grab username and password
	isAttemptedUserPassValidation := false
	reUser := regexp.MustCompile("(?i)^user:[a-zA-z0-9_\\-]+")
	user := reUser.FindString(cleanCmd)
	if len(user) > 0 {
		isAttemptedUserPassValidation = true
		// we found a username at the start of the exec command, use it
		cleanCmd = reUser.ReplaceAllString(cleanCmd, "")
		cleanCmd = strings.TrimPrefix(cleanCmd, " ")
		user = regexp.MustCompile("^user:").ReplaceAllString(user, "")
	}
	rePass := regexp.MustCompile("(?i)^pass:[a-zA-z0-9_\\-]+")
	pass := rePass.FindString(cleanCmd)
	if len(pass) > 0 {
		// we found a username at the start of the exec command, use it
		cleanCmd = rePass.ReplaceAllString(cleanCmd, "")
		cleanCmd = strings.TrimPrefix(cleanCmd, " ")
		pass = regexp.MustCompile("^pass:").ReplaceAllString(pass, "")
	}

	// trim front and back of string
	cleanCmd = regexp.MustCompile("^\\s*").ReplaceAllString(cleanCmd, "")
	cleanCmd = regexp.MustCompile("\\s*$").ReplaceAllString(cleanCmd, "")
	line := cleanCmd
	argArr := []string{line}

	// OLD APPROACH
	// now we have to split off the first command and pass the rest as args
	/*
		cmdArr := strings.Split(cleanCmd, " ")
		cmd := cmdArr[0]
		argArr := cmdArr[1:]
		oscmd := exec.Command(cmd, argArr...)
	*/
	var cmd string
	var oscmd *exec.Cmd

	// allow user/pass authentication. assume not valid.
	isUserPassValid := false

	// NEW APPROACH borrowed from mattn/go-shellwords
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		cmd = shell
		oscmd = exec.Command(shell, "/c", line)
	} else {
		shell := os.Getenv("SHELL")
		cmd = shell
		oscmd = exec.Command(shell, "-c", line)

		// if posix, i.e. linux or mac just check password via ssh
		if len(user) > 0 && len(pass) > 0 && checkUserPass(user, pass) {
			// the password was valid
			isUserPassValid = true
			log.Printf("User/pass was valid for request")
		}
	}

	if isAttemptedUserPassValidation {
		if isUserPassValid == false {
			errMsg := fmt.Sprintf("User:%s and password were not valid so not able to execute cmd.", user)
			log.Println(errMsg)
			mapD := ExecCmd{ExecStatus: "Error", Id: id, Cmd: cmd, Args: argArr, Output: errMsg}
			mapB, _ := json.Marshal(mapD)
			h.broadcastSys <- mapB
			return
		} else {
			log.Println("User:%s and password were valid. Running command.", user)
		}
	} else if *isAllowExec == false {
		log.Printf("Error trying to execute terminal command. No user/pass provided or command line switch was not specified to allow exec command. Provide a valied username/password or restart spjs with -allowexec command line option to exec command.")
		//h.broadcastSys <- []byte("Trying to execute terminal command, but command line switch was not specified to allow for this. Restart spjs with -allowexec command line option to enable.\n")
		mapD := ExecCmd{ExecStatus: "Error", Id: id, Cmd: cmd, Args: argArr, Output: "Error trying to execute terminal command. No user/pass provided or command line switch was not specified to allow exec command. Provide a valied username/password or restart spjs with -allowexec command line option to exec command."}
		mapB, _ := json.Marshal(mapD)
		h.broadcastSys <- mapB
		return
	} else {
		log.Println("Running cmd cuz -allowexec specified as command line option.")
	}

	// OLD APPROACH where we would queue up entire command and wait until done
	// will block here until results are done
	//cmdOutput, err := oscmd.CombinedOutput()

	//endProgress()

	// NEW APPROACH. Stream stdout while it is running so we get more real-time updates.
	cmdOutput := ""
	cmdReader, err := oscmd.StdoutPipe()
	if err != nil {
		log.Println(os.Stderr, "Error creating StdoutPipe for Cmd", err)
		//os.Exit(1)
		mapD := ExecCmd{ExecStatus: "Error", Id: id, Cmd: cmd, Args: argArr, Output: err.Error()}
		mapB, _ := json.Marshal(mapD)
		h.broadcastSys <- mapB
		return
	}

	scanner := bufio.NewScanner(cmdReader)
	go func() {
		for scanner.Scan() {
			log.Printf("stdout > %s\n", scanner.Text())
			mapD := ExecCmd{ExecStatus: "Progress", Id: id, Cmd: cmd, Args: argArr, Output: scanner.Text()}
			mapB, _ := json.Marshal(mapD)
			h.broadcastSys <- mapB
			cmdOutput += scanner.Text()
		}
	}()

	err = oscmd.Start()
	if err != nil {
		log.Println(os.Stderr, "Error starting Cmd", err)
		//os.Exit(1)
		mapD := ExecCmd{ExecStatus: "Error", Id: id, Cmd: cmd, Args: argArr, Output: fmt.Sprintf("Error starting Cmd", err)}
		mapB, _ := json.Marshal(mapD)
		h.broadcastSys <- mapB
		return
	}

	// block here until command done
	err = oscmd.Wait()
	/*if err != nil {
			fmt.Fprintln(os.Stderr, "Error waiting for Cmd", err)
	//		os.Exit(1)
			mapD := ExecCmd{ExecStatus: "Error", Id: id, Cmd: cmd, Args: argArr, Output: fmt.Sprintf(os.Stderr, "Error waiting for Cmd", err)}
			mapB, _ := json.Marshal(mapD)
			h.broadcastSys <- mapB
			return
		}*/

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

func checkUserPass(user string, pass string) bool {
	// We check the validity of the username/password
	// An SSH client is represented with a ClientConn. Currently only
	// the "password" authentication method is supported.
	//
	// To authenticate with the remote server you must pass at least one
	// implementation of AuthMethod via the Auth field in ClientConfig.
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(pass),
		},
	}
	client, err := ssh.Dial("tcp", "localhost:22", config)
	if err != nil {
		log.Println("Failed to dial: " + err.Error())
		return false
	}

	// Each ClientConn can support multiple interactive sessions,
	// represented by a Session.
	session, err := client.NewSession()
	if err != nil {
		log.Println("Failed to create session: " + err.Error())
		return false
	}
	defer session.Close()

	// Once a Session is created, you can execute a single command on
	// the remote side using the Run method.
	/*
		var b bytes.Buffer
		session.Stdout = &b
		if err := session.Run("echo spjs-authenticated"); err != nil {
			log.Println("Failed to run: " + err.Error())
			return false
		}
		log.Println(b.String())
	*/
	return true
}
