package main

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/walle/targz"
)

func main() {
	c, err := readConfig()
	if err != nil {
		log.Println("ERRR", err)
		return
	}
	log.Printf("INFO Initializing application\n")
	app, err := NewApplication(c)
	if err != nil {
		log.Println("ERRR", err)
		return
	}
	defer app.Close()
	log.Writer().Write([]byte("\n"))
	app.Run()
}

func (app *Application) makeBackup() error {
	if err := app.clearDumpDirectory(); err != nil {
		return err
	}

	if err := app.mysqlShellBackup(); err != nil {
		return err
	}

	now := time.Now().UTC().Format("2006-01-02_15:04:05")
	backupPath := filepath.Join(app.config.Directories.Backups, now+".tar.gz")
	log.Printf("INFO Compressing %q directory into .tar.gz archive\n", dumpDir)
	if err := targz.Compress(dumpDir, backupPath); err != nil {
		return err
	}

	if err := app.UploadToS3(backupPath); err != nil {
		return err
	}

	if err := app.removeOldArchives(); err != nil {
		return err
	}

	return nil
}

func (app *Application) mysqlShellBackup() error {
	log.Println("INFO Getting active cluster nodes from MySQL Router REST API")
	node, err := app.PickNode()
	if err != nil {
		return err
	}
	data := TemplateData{
		Host:          node,
		User:          app.config.Cluster.BackupUser,
		Password:      app.config.Cluster.BackupUserPassword,
		DumpDirectory: dumpDir,
	}
	var buf bytes.Buffer
	if err := pythonScriptTemplate.Execute(&buf, &data); err != nil {
		return err
	}

	log.Printf("INFO Writing %q script file\n", pythonScriptPath)
	if err = ioutil.WriteFile(pythonScriptPath, buf.Bytes(), os.ModePerm); err != nil {
		return err
	}

	cmd := exec.Command("mysqlsh", "--file", pythonScriptPath)

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	log.Printf("INFO Making dump of %q instance\n", node)
	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(outputBuffer.String())
		if strings.Contains(output, "(111)") { // can't connect to MySQL server
			log.Printf("[WARN] Instance %q is not available\n", node)
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
