package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/walle/targz"
)

func (app *Application) makeBackup() {
	if err := app.clearDumpDirectory(); err != nil {
		fmt.Println(err)
		return
	}

	if err := app.mysqlShellBackup(); err != nil {
		fmt.Println(err)
		return
	}

	now := time.Now().UTC().Format("2006-01-02_15:04:05")

	if err := targz.Compress(dumpDir, backupsDir+"/"+now+".tar.gz"); err != nil {
		fmt.Println(err)
		return
	}

	app.removeOldArchives()
}

func main() {
	c, err := readConfig()
	if err != nil {
		fmt.Println(err)
		return
	}

	app := NewApplication(c)

	app.Run()
}

func (app *Application) mysqlShellBackup() error {
	host, err := app.config.PickHost()
	if err != nil {
		return err
	}
	data := TemplateData{
		Host:          host,
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

	fmt.Println("Execute with", host)

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
	app.config.roundRobinCounter--
	return nil
}
