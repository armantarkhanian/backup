package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

var (
	timeFormat       = "2006-01-02_15-04-05"
	dumpDir          = "/tmp/dump"
	pythonScriptPath = "/tmp/backup.py"
)

type telegram struct {
	Turn     bool   `yaml:"turn"`
	BotToken string `yaml:"bot-token"`
	ChatID   int64  `yaml:"chat-id"`
}

type alertLevel string

const (
	Info  alertLevel = "INFO"
	Error alertLevel = "ERROR"
)

type alerts struct {
	Level    alertLevel `yaml:"level"`
	Telegram telegram   `yaml:"telegram"`
}

type directories struct {
	Backups string `yaml:"backups"`
	Logs    string `yaml:"logs"`
}

type cluster struct {
	Name               string `yaml:"name"`
	BackupUser         string `yaml:"backup-user"`
	BackupUserPassword string `yaml:"backup-user-password"`
}

type backup struct {
	Interval       time.Duration `yaml:"interval"`
	MaxBackupFiles int           `yaml:"max-backup-files"`
}

type mySQLRouter struct {
	Addr      string    `yaml:"http-addr"`
	BasicAuth basicAuth `yaml:"basic-auth"`
}

type basicAuth struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type s3 struct {
	Bucket          string `yaml:"bucket"`
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access-key-id"`
	SecretAccessKey string `yaml:"secret-access-key"`
	UseSSL          bool   `yaml:"use-ssl"`
}

type Config struct {
	Directories       directories `yaml:"directories"`
	Cluster           cluster     `yaml:"cluster"`
	Backup            backup      `yaml:"backup"`
	MySQLRouter       mySQLRouter `yaml:"mysqlrouter"`
	Alerts            alerts      `yaml:"alerts"`
	S3                s3          `yaml:"s3"`
	Nodes             []node      `yaml:"-"`
	roundRobinCounter int         `yaml:"-"`
	LogFile           *os.File    `yaml:"-"`
}

func readConfig() (*Config, error) {
	bytes, err := ioutil.ReadFile("config.yml")
	if err != nil {
		return nil, err
	}
	var c Config

	if err = yaml.Unmarshal(bytes, &c); err != nil {
		return nil, err
	}
	if c.Backup.MaxBackupFiles < 1 {
		return nil, errors.New("invalid config: backup.max-backup-files can not be less than 1")
	}
	if c.Directories.Logs != "" {
		if err := os.MkdirAll(c.Directories.Logs, os.ModePerm); err != nil {
			return nil, err
		}
		info, err := os.Stat(c.Directories.Logs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("invalid config: %q is not directory", c.Directories.Logs)
		}
		logFile := filepath.Join(c.Directories.Logs, "log")
		file, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return nil, err
		}
		c.LogFile = file
		log.SetOutput(c.LogFile)
	}
	if c.Directories.Backups == "" {
		return nil, errors.New("invalid config: directories.backups can not be empty string")
	}

	if err := os.MkdirAll(c.Directories.Backups, os.ModePerm); err != nil {
		return nil, err
	}
	info, err := os.Stat(c.Directories.Backups)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("invalid config: %q is not directory", c.Directories.Backups)
	}

	if c.Cluster.Name == "" {
		return nil, errors.New("invalid config: cluster.name can not be empty string")
	}
	if c.Cluster.BackupUser == "" {
		return nil, errors.New("invalid config: cluster.user can not be empty string")
	}
	if c.Backup.Interval == 0 {
		return nil, errors.New("invalid config: backup.interval can not be zero")
	}
	if c.Backup.MaxBackupFiles == 0 {
		return nil, errors.New("invalid config: backup.max-backup-files can not be zero")
	}
	if c.MySQLRouter.Addr == "" {
		return nil, errors.New("invalid config: mysql-router-http-host can not be empty string")
	}

	c.Alerts.Level = alertLevel(strings.ToUpper(string(c.Alerts.Level)))

	if c.Alerts.Level != Info && c.Alerts.Level != Error {
		return nil, errors.New("invalid config: alerts.level can be info or error")
	}

	return &c, nil
}

type node struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func (n *node) String() string {
	return n.Address + ":" + strconv.Itoa(n.Port)
}

func (app *Application) updateNodes() error {
	if !strings.HasPrefix(app.config.MySQLRouter.Addr, "http://") && !strings.HasPrefix(app.config.MySQLRouter.Addr, "https://") {
		app.config.MySQLRouter.Addr = "http://" + app.config.MySQLRouter.Addr
	}
	req, err := http.NewRequest("GET", app.config.MySQLRouter.Addr+"/api/20190715/routes/"+app.config.Cluster.Name+"_ro/destinations", nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(app.config.MySQLRouter.BasicAuth.User, app.config.MySQLRouter.BasicAuth.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf(`HTTP %v: %q`, resp.StatusCode, string(data))
	}
	type response struct {
		Items []node `json:"items"`
	}
	var r response
	if err := yaml.Unmarshal(data, &r); err != nil {
		return err
	}
	app.config.Nodes = []node{}
	app.config.Nodes = append(app.config.Nodes, r.Items...)
	return nil
}

var ErrNoAvailableNode = errors.New("all cluster nodes are unavailable")

func (app *Application) PickNode() (string, error) {
	if err := app.updateNodes(); err != nil {
		return "", err
	}
	if len(app.config.Nodes) == 0 {
		return "", ErrNoAvailableNode
	}
	return app.config.Nodes[0].String(), nil
}
