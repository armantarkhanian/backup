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
	dumpDir          = "/tmp/dump"
	pythonScriptPath = "/tmp/backup.py"
)

type directories struct {
	Backups string `yaml:"backups"`
	Logs    string `yaml:"logs"`
}

type cluster struct {
	Name     string `yaml:"name"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type backup struct {
	Interval       time.Duration `yaml:"interval"`
	MaxBackupFiles int           `yaml:"max-backup-files"`
}

type basicAuth struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type Config struct {
	Directories          directories `yaml:"directories"`
	Cluster              cluster     `yaml:"cluster"`
	Backup               backup      `yaml:"backup"`
	MySQLRouterHTTPHost  string      `yaml:"mysql-router-http-host"`
	MySQLRouterBasicAuth basicAuth   `yaml:"mysql-router-basic-auth"`

	Nodes             []node   `yaml:"-"`
	roundRobinCounter int      `yaml:"-"`
	LogFile           *os.File `yaml:"-"`
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

	if c.Directories.Logs != "" {
		info, err := os.Stat(c.Directories.Logs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("invalid config: %q is not directory", c.Directories.Logs)
		}
		if err := os.MkdirAll(c.Directories.Logs, os.ModePerm); err != nil {
			return nil, err
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
	if c.Cluster.Name == "" {
		return nil, errors.New("invalid config: cluster.name can not be empty string")
	}
	if c.Cluster.User == "" {
		return nil, errors.New("invalid config: cluster.user can not be empty string")
	}
	if c.Backup.Interval == 0 {
		return nil, errors.New("invalid config: backup.interval can not be zero")
	}
	if c.Backup.MaxBackupFiles == 0 {
		return nil, errors.New("invalid config: backup.max-backup-files can not be zero")
	}
	if c.MySQLRouterHTTPHost == "" {
		return nil, errors.New("invalid config: mysql-router-http-host can not be empty string")
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

func (c *Config) updateNodes() error {
	if !strings.HasPrefix(c.MySQLRouterHTTPHost, "http://") && !strings.HasPrefix(c.MySQLRouterHTTPHost, "https://") {
		c.MySQLRouterHTTPHost = "http://" + c.MySQLRouterHTTPHost
	}
	req, err := http.NewRequest("GET", c.MySQLRouterHTTPHost+"/api/20190715/routes/"+c.Cluster.Name+"_ro/destinations", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.MySQLRouterBasicAuth.User, c.MySQLRouterBasicAuth.Password)
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
	c.Nodes = []node{}
	c.Nodes = append(c.Nodes, r.Items...)
	return nil
}

var ErrNoAvailableNode = errors.New("all cluster nodes are unavailable")

func (c *Config) PickNode() (string, error) {
	if err := c.updateNodes(); err != nil {
		return "", err
	}
	if len(c.Nodes) == 0 {
		return "", ErrNoAvailableNode
	}
	return c.Nodes[0].String(), nil
}
