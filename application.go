package main

import (
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	minio "github.com/minio/minio-go/v7"
)

type Application struct {
	config      *Config
	minioClient *minio.Client

	quit chan os.Signal
}

func NewApplication(c *Config) *Application {
	return &Application{
		config: c,
	}
}

func (app *Application) Run() {
	ticker := time.NewTicker(app.config.IntervalDuration)

	app.quit = make(chan os.Signal)

	signal.Notify(app.quit, syscall.SIGINT, syscall.SIGTERM)

	defer signal.Stop(app.quit)

	for {
		select {
		case <-ticker.C:
			app.makeBackup()

		case <-app.quit:
			app.config.LogFile.Close()

			log.Printf("[INFO] Gracefull shutdown.\n\n")
			return
		}
	}
}

func (app *Application) clearDumpDirectory() error {
	log.Printf("[INFO] Trying delete %q directory.\n", dumpDir)
	if err := os.RemoveAll(dumpDir); err != nil {
		log.Println("[ERRR]", err)
		return err
	}
	log.Printf("[INFO] Succesfully deleted %q directory.", dumpDir)
	log.Printf("[INFO] Trying create %q directory.\n", dumpDir)
	if err := os.MkdirAll(dumpDir, os.ModePerm); err != nil {
		log.Println("[ERRR]", err)
		return err
	}
	log.Printf("[INFO] Succesfully created %q directory.", dumpDir)
	return nil
}

type backupArchive struct {
	path      string
	name      string
	createdAt time.Time
}

func (app *Application) removeOldArchives() error {
	var backupArchives []backupArchive
	err := filepath.WalkDir(backupsDir, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".tar.gz") {
			return nil
		}

		t, err := time.Parse("2006-01-02_15:04:05", strings.TrimSuffix(name, ".tar.gz"))
		if err != nil {
			return nil
		}
		backupArchives = append(backupArchives, backupArchive{
			path:      path,
			name:      name,
			createdAt: t,
		})
		return nil
	})
	if err != nil {
		return err
	}
	if len(backupArchives) >= app.config.MaxBackupFiles {
		sort.Slice(backupArchives, func(i, j int) bool {
			return backupArchives[i].createdAt.After(backupArchives[j].createdAt)
		})
		for i := app.config.MaxBackupFiles; i < len(backupArchives); i++ {
			if err := os.Remove(backupArchives[i].path); err != nil {
				return err
			}
		}
	}
	return nil
}
