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
			log.Println("-----------------------------------")

		case <-app.quit:
			log.Println("Gracefull shutdown")
			return
		}
	}
}

func (app *Application) clearDumpDirectory() error {
	if err := os.RemoveAll(dumpDir); err != nil {
		return err
	}
	return os.MkdirAll(dumpDir, os.ModePerm)
}

type backupArchive struct {
	path      string
	name      string
	createdAt time.Time
}

func (app *Application) removeOldArchives() {
	var backupArchives []backupArchive
	filepath.WalkDir(backupsDir, func(path string, d fs.DirEntry, err error) error {
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
	if len(backupArchives) >= app.config.MaxBackupFiles {
		sort.Slice(backupArchives, func(i, j int) bool {
			return backupArchives[i].createdAt.After(backupArchives[j].createdAt)
		})
		for i := app.config.MaxBackupFiles; i < len(backupArchives); i++ {
			if err := os.Remove(backupArchives[i].path); err != nil {
				log.Println("Error:", err)
			}
		}
	}
}
