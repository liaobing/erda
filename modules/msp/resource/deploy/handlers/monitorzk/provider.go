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

package monitorzk

import (
	"github.com/jinzhu/gorm"

	"github.com/erda-project/erda-infra/base/logs"
	"github.com/erda-project/erda-infra/base/servicehub"
	"github.com/erda-project/erda/modules/msp/resource/deploy/handlers"
)

type config struct {
}

// +provider
type provider struct {
	*handlers.DefaultDeployHandler
	Cfg *config
	Log logs.Logger
	DB  *gorm.DB `autowired:"mysql-client"`
}

func (p *provider) Init(ctx servicehub.Context) error {
	p.DefaultDeployHandler = handlers.NewDefaultHandler(p.DB)
	return nil
}

func init() {
	servicehub.Register("erda.msp.resource.deploy.handlers.monitorzk", &servicehub.Spec{
		Services: []string{
			"erda.msp.resource.deploy.handlers.monitorzk",
		},
		Description: "erda.msp.resource.deploy.handlers.monitorzk",
		ConfigFunc: func() interface{} {
			return &config{}
		},
		Creator: func() servicehub.Provider {
			return &provider{}
		},
	})
}
