/**
 * Tencent is pleased to support the open source community by making Polaris available.
 *
 * Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.
 *
 * Licensed under the BSD 3-Clause License (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://opensource.org/licenses/BSD-3-Clause
 *
 * Unless required by applicable law or agreed to in writing, software distributed
 * under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
 * CONDITIONS OF ANY KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations under the License.
 */

package boltdb

import (
	"time"

	"github.com/boltdb/bolt"
	"go.uber.org/zap"

	api "github.com/polarismesh/polaris/common/api/v1"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/common/utils"
	"github.com/polarismesh/polaris/store"
)

const (
	STORENAME = "boltdbStore"
)

type boltStore struct {
	*namespaceStore
	*clientStore

	// 服务注册发现、治理
	*serviceStore
	*instanceStore
	*l5Store
	*routingStore
	*rateLimitStore
	*circuitBreakerStore

	// 工具
	*toolStore

	// 鉴权模块相关
	*userStore
	*groupStore
	*strategyStore

	// 配置中心stores
	*configFileGroupStore
	*configFileStore
	*configFileReleaseStore
	*configFileReleaseHistoryStore
	*configFileTagStore
	*configFileTemplateStore

	// v2 存储
	*routingStoreV2

	// maintain store
	*maintainStore

	handler BoltHandler
	start   bool
}

// Name store name
func (m *boltStore) Name() string {
	return STORENAME
}

// Initialize init store
func (m *boltStore) Initialize(c *store.Config) error {
	if m.start {
		return nil
	}
	boltConfig := &BoltConfig{}
	boltConfig.Parse(c.Option)
	handler, err := NewBoltHandler(boltConfig)
	if err != nil {
		return err
	}
	m.handler = handler
	if err = m.newStore(); err != nil {
		_ = handler.Close()
		return err
	}

	if err = m.initAuthStoreData(); err != nil {
		_ = handler.Close()
		return err
	}

	if err = m.initNamingStoreData(); err != nil {
		_ = handler.Close()
		return err
	}
	m.start = true
	return nil
}

const (
	namespacePolaris = "Polaris"
	ownerToInit      = "polaris"
)

var (
	namespacesToInit = []string{"default", namespacePolaris}
	servicesToInit   = map[string]string{
		"polaris.checker": "fbca9bfa04ae4ead86e1ecf5811e32a9",
	}

	mainUser = &model.User{
		ID:          "65e4789a6d5b49669adf1e9e8387549c",
		Name:        "polaris",
		Password:    "$2a$10$3izWuZtE5SBdAtSZci.gs.iZ2pAn9I8hEqYrC6gwJp1dyjqQnrrum",
		Owner:       "",
		Source:      "Polaris",
		Mobile:      "",
		Email:       "",
		Type:        20,
		Token:       "nu/0WRA4EqSR1FagrjRj0fZwPXuGlMpX+zCuWu4uMqy8xr1vRjisSbA25aAC3mtU8MeeRsKhQiDAynUR09I=",
		TokenEnable: true,
		Valid:       true,
		Comment:     "default polaris admin account",
		CreateTime:  time.Now(),
		ModifyTime:  time.Now(),
	}

	mainDefaultStrategy = &model.StrategyDetail{
		ID:      "fbca9bfa04ae4ead86e1ecf5811e32a9",
		Name:    "(用户) polaris的默认策略",
		Action:  "READ_WRITE",
		Comment: "default admin",
		Principals: []model.Principal{
			{
				StrategyID:    "fbca9bfa04ae4ead86e1ecf5811e32a9",
				PrincipalID:   "65e4789a6d5b49669adf1e9e8387549c",
				PrincipalRole: model.PrincipalUser,
			},
		},
		Default: true,
		Owner:   "65e4789a6d5b49669adf1e9e8387549c",
		Resources: []model.StrategyResource{
			{
				StrategyID: "fbca9bfa04ae4ead86e1ecf5811e32a9",
				ResType:    int32(api.ResourceType_Namespaces),
				ResID:      "*",
			},
			{
				StrategyID: "fbca9bfa04ae4ead86e1ecf5811e32a9",
				ResType:    int32(api.ResourceType_Services),
				ResID:      "*",
			},
			{
				StrategyID: "fbca9bfa04ae4ead86e1ecf5811e32a9",
				ResType:    int32(api.ResourceType_ConfigGroups),
				ResID:      "*",
			},
		},
		Valid:      true,
		Revision:   "fbca9bfa04ae4ead86e1ecf5811e32a9",
		CreateTime: time.Now(),
		ModifyTime: time.Now(),
	}
)

func (m *boltStore) initNamingStoreData() error {
	for _, namespace := range namespacesToInit {
		curTime := time.Now()
		err := m.AddNamespace(&model.Namespace{
			Name:       namespace,
			Token:      utils.NewUUID(),
			Owner:      ownerToInit,
			Valid:      true,
			CreateTime: curTime,
			ModifyTime: curTime,
		})
		if err != nil {
			return err
		}
	}
	for svc, id := range servicesToInit {
		curTime := time.Now()
		err := m.AddService(&model.Service{
			ID:         id,
			Name:       svc,
			Namespace:  namespacePolaris,
			Token:      utils.NewUUID(),
			Owner:      ownerToInit,
			Revision:   utils.NewUUID(),
			Valid:      true,
			CreateTime: curTime,
			ModifyTime: curTime,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *boltStore) initAuthStoreData() error {
	return m.handler.Execute(true, func(tx *bolt.Tx) error {
		user, err := m.getUser(tx, mainUser.ID)
		if err != nil {
			return err
		}

		if user == nil {
			user = mainUser
			// 添加主账户主体信息
			if err := saveValue(tx, tblUser, user.ID, converToUserStore(user)); err != nil {
				authLog.Error("[Store][User] save user fail", zap.Error(err), zap.String("name", user.Name))
				return err
			}
		}

		rule, err := m.getStrategyDetail(tx, mainDefaultStrategy.ID)
		if err != nil {
			return err
		}

		if rule == nil {
			strategy := mainDefaultStrategy
			// 添加主账户的默认鉴权策略信息
			if err := saveValue(tx, tblStrategy, strategy.ID, convertForStrategyStore(strategy)); err != nil {
				authLog.Error("[Store][Strategy] save auth_strategy", zap.Error(err),
					zap.String("name", strategy.Name), zap.String("owner", strategy.Owner))
				return err
			}
		}
		return nil
	})
}

func (m *boltStore) newStore() error {
	var err error

	m.l5Store = &l5Store{handler: m.handler}
	if err = m.l5Store.InitL5Data(); err != nil {
		return err
	}
	m.namespaceStore = &namespaceStore{handler: m.handler}
	if err = m.namespaceStore.InitData(); err != nil {
		return err
	}
	m.clientStore = &clientStore{handler: m.handler}

	if err := m.newDiscoverModuleStore(); err != nil {
		return err
	}
	if err := m.newAuthModuleStore(); err != nil {
		return err
	}
	if err := m.newConfigModuleStore(); err != nil {
		return err
	}

	if err := m.newMaintainModuleStore(); err != nil {
		return err
	}

	return nil
}

func (m *boltStore) newDiscoverModuleStore() error {
	m.serviceStore = &serviceStore{handler: m.handler}

	m.instanceStore = &instanceStore{handler: m.handler}

	m.routingStore = &routingStore{handler: m.handler}

	m.rateLimitStore = &rateLimitStore{handler: m.handler}

	m.circuitBreakerStore = &circuitBreakerStore{handler: m.handler}

	m.routingStoreV2 = &routingStoreV2{handler: m.handler}

	return nil
}

func (m *boltStore) newAuthModuleStore() error {
	m.userStore = &userStore{handler: m.handler}

	m.strategyStore = &strategyStore{handler: m.handler}

	m.groupStore = &groupStore{handler: m.handler}

	return nil
}

func (m *boltStore) newConfigModuleStore() error {
	var err error

	m.configFileStore, err = newConfigFileStore(m.handler)
	if err != nil {
		return err
	}

	m.configFileTagStore, err = newConfigFileTagStore(m.handler)
	if err != nil {
		return err
	}

	m.configFileGroupStore, err = newConfigFileGroupStore(m.handler)
	if err != nil {
		return err
	}

	m.configFileReleaseHistoryStore, err = newConfigFileReleaseHistoryStore(m.handler)
	if err != nil {
		return err
	}

	m.configFileReleaseStore, err = newConfigFileReleaseStore(m.handler)
	if err != nil {
		return err
	}

	m.configFileTemplateStore, err = newConfigFileTemplateStore(m.handler)
	if err != nil {
		return err
	}

	return nil
}

func (m *boltStore) newMaintainModuleStore() error {
	m.maintainStore = &maintainStore{handler: m.handler}

	return nil
}

// Destroy store
func (m *boltStore) Destroy() error {
	m.start = false
	if m.handler != nil {
		return m.handler.Close()
	}
	return nil
}

// CreateTransaction create store transaction
func (m *boltStore) CreateTransaction() (store.Transaction, error) {
	return &transaction{handler: m.handler}, nil
}

// StartTx starting transactions
func (m *boltStore) StartTx() (store.Tx, error) {
	return m.handler.StartTx()
}

func init() {
	s := &boltStore{}
	_ = store.RegisterStore(s)
}
