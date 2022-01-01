package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
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
	ClusterName    string      `json:"clusterName"`
	User           string      `json:"user"`
	Password       string      `json:"password"`
	MySQLRouter    mysqlRouter `json:"mysqlrouter"`
	IntervalString string      `json:"interval"`
	MaxBackupFiles int         `json:"maxBackupFiles"`

	Nodes             []node        `json:"-"`
	roundRobinCounter int           `json:"-"`
	IntervalDuration  time.Duration `json:"-"`
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
	return &c, nil
}

type node struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func (n *node) String() string {
	return n.Address + ":" + strconv.Itoa(n.Port)
}

func (c *Config) updateNodes() {
	if !strings.HasPrefix(c.MySQLRouter.Addr, "http://") && !strings.HasPrefix(c.MySQLRouter.Addr, "https://") {
		c.MySQLRouter.Addr = "http://" + c.MySQLRouter.Addr
	}
	req, err := http.NewRequest("GET", c.MySQLRouter.Addr+"/api/20190715/routes/myCluster_ro/destinations", nil)
	if err != nil {
		log.Println("Error:", err)
		return
	}
	req.SetBasicAuth(c.MySQLRouter.BasicAuth.User, c.MySQLRouter.BasicAuth.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error:", err)
		return
	}
	defer resp.Body.Close()
	type response struct {
		Items []node `json:"items"`
	}
	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		log.Println("Error with status code:", resp.StatusCode, err)
		return
	}
	c.Nodes = []node{}
	c.Nodes = append(c.Nodes, r.Items...)
	log.Println("Got node list:", c.Nodes)
}

var ErrNoAvailableNode = errors.New("all cluster nodes are unavailable")

func (c *Config) PickNode() (string, error) {
	log.Println("Started updating node list...")
	c.updateNodes()
	log.Println("Ended updating node list.")
	if len(c.Nodes) == 0 {
		return "", ErrNoAvailableNode
	}
	return c.Nodes[0].String(), nil
}
