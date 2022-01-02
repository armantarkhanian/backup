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

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Application struct {
	config      *Config
	minioClient *minio.Client
	telegramBot *tgbotapi.BotAPI
	quit        chan os.Signal
}

func (app *Application) SendTelegram(message string) error {
	msg := tgbotapi.NewMessage(app.config.Alerts.Telegram.ChatID, message)
	msg.ParseMode = "markdown"
	_, err := app.telegramBot.Send(msg)
	return err
}

func NewApplication(c *Config) (*Application, error) {
	minioClient, err := minio.New(c.S3.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.S3.AccessKeyID, c.S3.SecretAccessKey, ""),
		Secure: c.S3.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	bot, err := tgbotapi.NewBotAPI(c.Alerts.Telegram.BotToken)
	if err != nil {
		return nil, err
	}

	bot.Debug = false

	return &Application{
		config:      c,
		minioClient: minioClient,
		telegramBot: bot,
	}, nil
}

func (app *Application) Close() {
	if app.config.LogFile != nil {
		app.config.LogFile.Close()
	}
}

func (app *Application) Run() {
	ticker := time.NewTicker(app.config.Backup.Interval)

	app.quit = make(chan os.Signal)

	signal.Notify(app.quit, syscall.SIGINT, syscall.SIGTERM)

	defer signal.Stop(app.quit)

	for {
		select {
		case <-ticker.C:
			if err := app.makeBackup(); err != nil {
				log.Printf("[ERRR] %s\n\n", err.Error())
				return
			}

		case <-app.quit:
			log.Printf("[INFO] Gracefull shutdown.\n\n")
			app.Close()
			return
		}
	}
}

func (app *Application) clearDumpDirectory() error {
	log.Printf("[INFO] Trying delete %q directory.\n", dumpDir)
	if err := os.RemoveAll(dumpDir); err != nil {
		return err
	}
	log.Printf("[INFO] Succesfully deleted %q directory.", dumpDir)
	log.Printf("[INFO] Trying create %q directory.\n", dumpDir)
	if err := os.MkdirAll(dumpDir, os.ModePerm); err != nil {
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
	err := filepath.WalkDir(app.config.Directories.Backups, func(path string, d fs.DirEntry, err error) error {
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
	if len(backupArchives) >= app.config.Backup.MaxBackupFiles {
		sort.Slice(backupArchives, func(i, j int) bool {
			return backupArchives[i].createdAt.After(backupArchives[j].createdAt)
		})
		for i := app.config.Backup.MaxBackupFiles; i < len(backupArchives); i++ {
			if err := os.Remove(backupArchives[i].path); err != nil {
				return err
			}
		}
	}
	return nil
}
