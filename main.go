package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/walle/targz"
)

func (app *Application) makeBackup() {
	log.Println("Started clear dumpDirectory...")
	if err := app.clearDumpDirectory(); err != nil {
		log.Println("Error:", err)
		return
	}
	log.Println("Ended clear dumpDirectory")

	log.Println("Started mysql-shell backup script...")
	if err := app.mysqlShellBackup(); err != nil {
		log.Println("Error:", err)
		return
	}
	log.Println("Ended mysql-shell backup script")

	now := time.Now().UTC().Format("2006-01-02_15:04:05")

	log.Println("Started creating .tar.gz archive")
	if err := targz.Compress(dumpDir, backupsDir+"/"+now+".tar.gz"); err != nil {
		log.Println("Error:", err)
		return
	}
	log.Println("Ended creating .tar.gz archive")

	log.Println("Started removing old archives")
	app.removeOldArchives()
	log.Println("Ended removing old archives")
}

func main() {
	log.Println("Started read config.json")
	c, err := readConfig()
	if err != nil {
		log.Println("Error:", err)
		return
	}
	log.Println("Ended read config.json")
	log.Println("-----------------------------------")

	app := NewApplication(c)

	app.Run()
}

func (app *Application) mysqlShellBackup() error {
	node, err := app.config.PickNode()
	if err != nil {
		return err
	}
	data := TemplateData{
		Host:          node,
		Password:      app.config.Password,
		DumpDirectory: dumpDir,
	}
	var buf bytes.Buffer
	if err := pythonScriptTemplate.Execute(&buf, &data); err != nil {
		return err
	}
	if err = ioutil.WriteFile(pythonScriptPath, buf.Bytes(), os.ModePerm); err != nil {
		return err
	}

	cmd := exec.Command("mysqlsh", "--file", pythonScriptPath)

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	fmt.Println("Execute with", node)

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(outputBuffer.String())
		if strings.Contains(output, "(111)") { // can't connect to MySQL server
			if app.config.roundRobinCounter == len(app.config.Nodes) {
				// if all cluster nodes are unavailable, then return
				return ErrNoAvailableNode
			}
			return app.mysqlShellBackup()
		}
		return errors.New(output)
	}

	app.config.roundRobinCounter-- // stay at this node if it's available

	return nil
}
