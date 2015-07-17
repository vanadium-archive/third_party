// Copyright 2011-2015 visualfc <visualfc@gmail.com>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runcmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/visualfc/gotools/command"
)

var Command = &command.Command{
	Run:       runCmd,
	UsageLine: "runcmd [-w work_path] <program_name> [arguments...]",
	Short:     "run program",
	Long:      `run program and arguments`,
}

var execWorkPath string
var execWaitEnter bool

func init() {
	Command.Flag.StringVar(&execWorkPath, "w", "", "work path")
	Command.Flag.BoolVar(&execWaitEnter, "e", true, "wait enter and continue")
}

func runCmd(cmd *command.Command, args []string) error {
	if len(args) == 0 {
		cmd.Usage()
		return os.ErrInvalid
	}
	if execWorkPath == "" {
		var err error
		execWorkPath, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "liteide_stub exec: os.Getwd() false\n")
			command.SetExitStatus(3)
			command.Exit()
			return err
		}
	}
	fileName := args[0]

	filePath, err := exec.LookPath(fileName)
	if err != nil {
		filePath, err = exec.LookPath("./" + fileName)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "liteide_stub exec: file %s not found\n", fileName)
		command.SetExitStatus(3)
		command.Exit()
	}

	fmt.Println("Starting Process", filePath, strings.Join(args[1:], " "), "...")

	command := exec.Command(filePath, args[1:]...)
	command.Dir = execWorkPath
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	err = command.Run()

	if err != nil {
		fmt.Println("\nEnd Process", err)
	} else {
		fmt.Println("\nEnd Process", "exit status 0")
	}

	exitWaitEnter()
	return nil
}

func exitWaitEnter() {
	if !execWaitEnter {
		return
	}
	fmt.Println("\nPress enter key to continue")
	var s = [256]byte{}
	os.Stdin.Read(s[:])
	command.SetExitStatus(0)
	command.Exit()
}
