package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
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

var (
	ErrNilTelegramBot = errors.New("telegram bot is nil")
	ErrNilMinioClient = errors.New("minio client is nil")
)

func (app *Application) SendTelegram(message string, parseMode string) error {
	if app.telegramBot == nil {
		return ErrNilTelegramBot
	}
	msg := tgbotapi.NewMessage(app.config.Alerts.Telegram.ChatID, message)
	if parseMode != "" {
		msg.ParseMode = parseMode
	}
	_, err := app.telegramBot.Send(msg)
	return err
}

func (app *Application) UploadToS3(path string) error {
	if app.minioClient == nil {
		return ErrNilMinioClient
	}
	log.Printf("INFO Cheking if %q bucket exists", app.config.S3.Bucket)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bucketExists, err := app.minioClient.BucketExists(ctx, app.config.S3.Bucket)
	if err != nil {
		return err
	}
	if !bucketExists {
		log.Printf("INFO Creating bucket %q.", app.config.S3.Bucket)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.minioClient.MakeBucket(ctx, app.config.S3.Bucket, minio.MakeBucketOptions{}); err != nil {
			return err
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	log.Println("INFO Uploading backup file into S3 Storage")
	_, err = app.minioClient.FPutObject(
		ctx,
		app.config.S3.Bucket,
		filepath.Base(path),
		path,
		minio.PutObjectOptions{},
	)

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
	bot, err := tgbotapi.NewBotAPIWithClient(c.Alerts.Telegram.BotToken, tgbotapi.APIEndpoint, &http.Client{
		Timeout: 10 * time.Second,
	})
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

func (app *Application) Alert(message string, telegramParseMode string) {
	if app.config.Alerts.Telegram.Turn {
		log.Println("INFO Sending telegram alert")
		if err := app.SendTelegram(message, telegramParseMode); err != nil {
			log.Println("ERRR", err)
		}
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
			err := app.makeBackup()
			if err != nil {
				log.Printf("ERRR %s\n", err.Error())
				app.Alert(fmt.Sprintf("ERROR %s", err.Error()), "")
			} else {
				app.Alert("Succes", "")
			}
			log.Writer().Write([]byte("\n"))

		case <-app.quit:
			log.Printf("INFO Gracefull shutdown.\n\n")
			app.Close()
			return
		}
	}
}

func (app *Application) clearDumpDirectory() error {
	log.Printf("INFO Removing %q directory.\n", dumpDir)
	if err := os.RemoveAll(dumpDir); err != nil {
		return err
	}
	log.Printf("INFO Creating %q directory.\n", dumpDir)
	if err := os.MkdirAll(dumpDir, os.ModePerm); err != nil {
		return err
	}
	return nil
}

type backupArchive struct {
	path      string
	name      string
	createdAt time.Time
}

func (app *Application) removeLocal() error {
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
	if len(backupArchives) > app.config.Backup.MaxBackupFiles {
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

func (app *Application) removeFromS3() error {
	if app.minioClient == nil {
		return ErrNilMinioClient
	}
	var backupArchives []minio.ObjectInfo
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for object := range app.minioClient.ListObjects(
		ctx,
		app.config.S3.Bucket,
		minio.ListObjectsOptions{},
	) {
		backupArchives = append(backupArchives, object)
	}
	if len(backupArchives) > app.config.Backup.MaxBackupFiles {
		sort.Slice(backupArchives, func(i, j int) bool {
			return backupArchives[i].LastModified.After(backupArchives[j].LastModified)
		})
		for i := app.config.Backup.MaxBackupFiles; i < len(backupArchives); i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := app.minioClient.RemoveObject(
				ctx,
				app.config.S3.Bucket,
				backupArchives[i].Key,
				minio.RemoveObjectOptions{},
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (app *Application) removeOldArchives() error {
	log.Println("INFO Removing old backups from local")
	if err := app.removeLocal(); err != nil {
		return err
	}
	log.Println("INFO Removing old backups from S3 Storage")
	if err := app.removeFromS3(); err != nil {
		return err
	}
	return nil
}
