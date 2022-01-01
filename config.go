package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	dumpDir          = "/tmp/dump"
	backupsDir       = "/home/arman/backups"
	pythonScriptPath = "/tmp/backup.py"
	endpoint         = "/api/20190715/routes/myCluster_ro/destinations"
)

type mysqlRouter struct {
	Addr      string    `json:"addr"`
	BasicAuth basicAuth `json:"basicAuth"`
}

type basicAuth struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type Config struct {
	Log            string      `json:"log"`
	ClusterName    string      `json:"clusterName"`
	User           string      `json:"user"`
	Password       string      `json:"password"`
	MySQLRouter    mysqlRouter `json:"mysqlrouter"`
	IntervalString string      `json:"interval"`
	MaxBackupFiles int         `json:"maxBackupFiles"`

	Nodes             []node        `json:"-"`
	roundRobinCounter int           `json:"-"`
	IntervalDuration  time.Duration `json:"-"`
	LogFile           *os.File      `json:"-"`
}

func readConfig() (*Config, error) {
	bytes, err := ioutil.ReadFile("config.json")
	if err != nil {
		return nil, err
	}
	var c Config
	if err = json.Unmarshal(bytes, &c); err != nil {
		return nil, err
	}
	c.IntervalDuration, err = time.ParseDuration(c.IntervalString)
	if err != nil {
		return nil, err
	}

	if c.Log != "" {
		file, err := os.OpenFile(c.Log, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return nil, err
		}
		c.LogFile = file
		log.SetOutput(c.LogFile)
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
	if !strings.HasPrefix(c.MySQLRouter.Addr, "http://") && !strings.HasPrefix(c.MySQLRouter.Addr, "https://") {
		c.MySQLRouter.Addr = "http://" + c.MySQLRouter.Addr
	}
	req, err := http.NewRequest("GET", c.MySQLRouter.Addr+"/api/20190715/routes/"+c.ClusterName+"_ro/destinations", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.MySQLRouter.BasicAuth.User, c.MySQLRouter.BasicAuth.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	type response struct {
		Items []node `json:"items"`
	}
	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
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
