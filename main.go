package main

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/walle/targz"
)

func (app *Application) makeBackup() error {
	if err := app.clearDumpDirectory(); err != nil {
		return err
	}

	if err := app.mysqlShellBackup(); err != nil {
		return err
	}

	now := time.Now().UTC().Format("2006-01-02_15:04:05")
	backupPath := backupsDir + "/" + now + ".tar.gz"
	log.Printf("[INFO] Trying compress %q directory into .tar.gz archive\n", dumpDir)
	if err := targz.Compress(dumpDir, backupPath); err != nil {
		return err
	}
	log.Printf("[INFO] Succesfully created backup into %q.\n", backupPath)

	log.Printf("[INFO] Trying remove old archives in %q directory\n", backupsDir)
	if err := app.removeOldArchives(); err != nil {
		return err
	}
	log.Printf("[INFO] Succesfully removed old archives.\n\n")
	return nil
}

func main() {
	c, err := readConfig()
	if err != nil {
		log.Println("[ERRR]", err)
		return
	}
	log.Printf("[INFO] Succesfully read config file.\n\n")

	app := NewApplication(c)
	defer app.Close()

	app.Run()
}

func (app *Application) mysqlShellBackup() error {
	log.Println("[INFO] Making MySQL Router REST API call (to get active cluster nodes)")
	node, err := app.config.PickNode()
	if err != nil {
		return err
	}
	log.Printf("[INFO] Succesfully got active cluster nodes: %v.\n", app.config.Nodes)
	data := TemplateData{
		Host:          node,
		User:          app.config.User,
		Password:      app.config.Password,
		DumpDirectory: dumpDir,
	}
	var buf bytes.Buffer
	if err := pythonScriptTemplate.Execute(&buf, &data); err != nil {
		return err
	}

	log.Printf("[INFO] Trying write %q script file\n", pythonScriptPath)
	if err = ioutil.WriteFile(pythonScriptPath, buf.Bytes(), os.ModePerm); err != nil {
		return err
	}
	log.Printf("[INFO] Succesfully wrote %q script file.\n", pythonScriptPath)

	cmd := exec.Command("mysqlsh", "--file", pythonScriptPath)

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	log.Printf("[INFO] Trying make dump of %q instance\n", node)
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
	log.Printf("[INFO] Succesfully dumped %q instance into %q directory.\n", node, dumpDir)
	app.config.roundRobinCounter-- // stay at this node if it's available

	return nil
}
