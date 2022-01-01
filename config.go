package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"time"
)

var (
	dumpDir          = "/tmp/dump"
	backupsDir       = "/home/arman/backups"
	pythonScriptPath = "/tmp/backup.py"
)

type Config struct {
	roundRobinCounter int           `json:"-"`
	IntervalDuration  time.Duration `json:"-"`
	IntervalString    string        `json:"interval"`
	MaxBackupFiles    int           `json:"maxBackupFiles"`
	Password          string        `json:"password"`
	Nodes             []string      `json:"nodes"`
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

var ErrNoAvailableNode = errors.New("all cluster nodes are unavailable")

func (c *Config) PickNode() (string, error) {
	if c.roundRobinCounter < 0 || c.roundRobinCounter >= len(c.Nodes) {
		return "", ErrNoAvailableNode
	}

	c.roundRobinCounter++

	return c.Nodes[c.roundRobinCounter-1], nil
}
