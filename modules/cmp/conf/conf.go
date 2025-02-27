// Copyright (c) 2021 Terminus, Inc.
//
// This program is free software: you can use, redistribute, and/or modify
// it under the terms of the GNU Affero General Public License, version 3
// or later ("AGPL"), as published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

// Package conf Define the configuration
package conf

import (
	"time"

	"github.com/erda-project/erda/pkg/envconf"
)

// Conf Define the configuration
type Conf struct {
	Debug             bool          `env:"DEBUG" default:"false"`
	EnableEss         bool          `env:"ENABLE_ESS" default:"false"`
	ListenAddr        string        `env:"LISTEN_ADDR" default:":9027"`
	SoldierAddr       string        `env:"SOLDIER_ADDR"`
	SchedulerAddr     string        `env:"SCHEDULER_ADDR"`
	TaskSyncDuration  time.Duration `env:"TASK_SYNC_DURATION" default:"2h"`
	TaskCleanDuration time.Duration `env:"TASK_CLEAN_DURATION" default:"24h"`
}

var cfg Conf

// Load Load envs
func Load() {
	envconf.MustLoad(&cfg)
}

// ListenAddr return the address of listen.
func ListenAddr() string {
	return cfg.ListenAddr
}

// SoldierAddr return the address of soldier.
func SoldierAddr() string {
	return cfg.SoldierAddr
}

// SchedulerAddr Return the address of scheduler.
func SchedulerAddr() string {
	return cfg.SchedulerAddr
}

// Debug Return the switch of debug.
func Debug() bool {
	return cfg.Debug
}

func EnableEss() bool {
	return cfg.EnableEss
}

func TaskSyncDuration() time.Duration {
	return cfg.TaskSyncDuration
}

func TaskCleanDuration() time.Duration {
	return cfg.TaskCleanDuration
}
