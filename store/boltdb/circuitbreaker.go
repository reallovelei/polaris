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
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"go.uber.org/zap"

	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/common/utils"
	"github.com/polarismesh/polaris/store"
)

const (
	// rule 相关信息以及映射
	tblCircuitBreaker string = "circuitbreaker_rule"

	// relation 相关信息以及映射信息
	tblCircuitBreakerRelation string = "circuitbreaker_rule_relation"
	VersionForMaster          string = "master"
	CBFieldNameValid          string = "Valid"
	CBFieldNameVersion        string = "Version"
	CBFieldNameID             string = "ID"
	CBFieldNameModifyTime     string = "ModifyTime"

	CBRFieldNameServiceID   string = "ServiceID"
	CBRFieldNameRuleID      string = "RuleID"
	CBRFieldNameRuleVersion string = "RuleVersion"

	CBRelationFieldServiceID   string = "ServiceID"
	CBRelationFieldRuleID      string = "RuleID"
	CBRelationFieldRuleVersion string = "RuleVersion"
	CBRelationFieldValid       string = "Valid"
	CBRelationFieldCreateTime  string = "CreateTime"
	CBRelationFieldModifyTime  string = "ModifyTime"
)

type circuitBreakerStore struct {
	handler BoltHandler
}

func initCircuitBreaker(cb *model.CircuitBreaker) {
	cb.Valid = true
	cb.CreateTime = time.Now()
	cb.ModifyTime = time.Now()
}

// CreateCircuitBreaker create circuit breaker rule
func (c *circuitBreakerStore) CreateCircuitBreaker(cb *model.CircuitBreaker) error {
	dbOp := c.handler

	initCircuitBreaker(cb)

	if err := c.cleanCircuitBreaker(cb.ID, cb.Version); err != nil {
		log.Errorf("[Store][circuitBreaker] clean master for circuit breaker(%s, %s) err: %s",
			cb.ID, cb.Version, err.Error())
		return store.Error(err)
	}

	if err := dbOp.SaveValue(tblCircuitBreaker, c.buildKey(cb.ID, cb.Version), cb); err != nil {
		log.Errorf("[Store][circuitBreaker] create circuit breaker(%s, %s, %s) err: %s",
			cb.ID, cb.Name, cb.Version, err.Error())
		return store.Error(err)
	}
	return nil
}

// cleanCircuitBreaker 彻底清理熔断规则
func (c *circuitBreakerStore) cleanCircuitBreaker(id string, version string) error {
	if err := c.handler.DeleteValues(tblCircuitBreaker, []string{c.buildKey(id, version)}); err != nil {
		log.Errorf("[Store][circuitBreaker] clean invalid circuit-breaker(%s, %s) err: %s",
			id, version, err.Error())
		return store.Error(err)
	}

	return nil
}

// TagCircuitBreaker 标记熔断规则
func (c *circuitBreakerStore) TagCircuitBreaker(cb *model.CircuitBreaker) error {
	if err := c.cleanCircuitBreaker(cb.ID, cb.Version); err != nil {
		log.Errorf("[Store][circuitBreaker] clean tag for circuit breaker(%s, %s) err: %s",
			cb.ID, cb.Version, err.Error())
		return store.Error(err)
	}

	if err := c.tagCircuitBreaker(cb); err != nil {
		log.Errorf("[Store][circuitBreaker] create tag for circuit breaker(%s, %s) err: %s",
			cb.ID, cb.Version, err.Error())
		return store.Error(err)
	}

	return nil
}

// tagCircuitBreaker
func (c *circuitBreakerStore) tagCircuitBreaker(cb *model.CircuitBreaker) error {
	dbOp := c.handler
	// first : Ensure that the master rule exists
	result, err := c.GetCircuitBreaker(cb.ID, VersionForMaster)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get tag rule id(%s) version(%s) err : %s",
			cb.ID, VersionForMaster, err.Error())
		return store.Error(err)
	}

	if result == nil {
		return store.NewStatusError(store.NotFoundCircuitBreaker, fmt.Sprintf("not exist for CircuitBreaker(id=%s, "+
			"version=%s)", cb.ID, VersionForMaster))
	}

	initCircuitBreaker(cb)
	if err := dbOp.SaveValue(tblCircuitBreaker, c.buildKey(cb.ID, cb.Version), cb); err != nil {
		log.Errorf("[Store][circuitBreaker] tag rule breaker(%s, %s, %s) err: %s",
			cb.ID, cb.Name, cb.Version, err.Error())
		return store.Error(err)
	}

	return nil
}

// ReleaseCircuitBreaker 发布熔断规则
func (c *circuitBreakerStore) ReleaseCircuitBreaker(cbr *model.CircuitBreakerRelation) error {
	if err := c.releaseCircuitBreaker(cbr); err != nil {
		log.Errorf("[Store][CircuitBreaker] release rule err: %s", err.Error())
		return store.Error(err)
	}

	return nil
}

// releaseCircuitBreaker 发布熔断规则的内部函数
// @note 可能存在服务的规则，由旧的更新到新的场景
func (c *circuitBreakerStore) releaseCircuitBreaker(cbr *model.CircuitBreakerRelation) error {
	// 上层调用者保证 service 是已经存在的
	dbOp := c.handler
	tRule, err := c.GetCircuitBreaker(cbr.RuleID, cbr.RuleVersion)
	if err != nil {
		return err
	}
	if tRule == nil {
		return store.NewStatusError(store.NotFoundMasterConfig, "not found tag config")
	}

	cbr.Valid = true
	cbr.CreateTime = time.Now()
	cbr.ModifyTime = time.Now()

	// 如果之前存在，就直接覆盖上一次的 release 信息
	if err := dbOp.SaveValue(tblCircuitBreakerRelation, cbr.ServiceID, cbr); err != nil {
		log.Errorf("[Store][circuitBreaker] tag rule relation(%s, %s, %s) err: %s",
			cbr.ServiceID, cbr.RuleID, cbr.RuleVersion, err.Error())
		return store.Error(err)
	}
	return nil
}

// UnbindCircuitBreaker 解绑熔断规则
func (c *circuitBreakerStore) UnbindCircuitBreaker(serviceID, ruleID, ruleVersion string) error {
	// 删除某个服务的熔断规则
	properties := make(map[string]interface{})
	properties[CBRelationFieldValid] = false
	properties[CBRelationFieldModifyTime] = time.Now()

	if err := c.handler.UpdateValue(tblCircuitBreakerRelation, serviceID, properties); err != nil {
		log.Errorf("[Store][circuitBreaker] tag rule relation(%s, %s, %s) err: %s",
			serviceID, ruleID, ruleVersion, err.Error())
		return store.Error(err)
	}
	return nil
}

// DeleteTagCircuitBreaker 删除已标记熔断规则
func (c *circuitBreakerStore) DeleteTagCircuitBreaker(id string, version string) error {
	err := c.handler.Execute(true, func(tx *bolt.Tx) error {
		values := make(map[string]interface{})
		fields := []string{CBRelationFieldValid, CBRelationFieldRuleID, CBRelationFieldRuleVersion}

		err := loadValuesByFilter(tx, tblCircuitBreakerRelation, fields, &model.CircuitBreakerRelation{},
			func(m map[string]interface{}) bool {
				if valid, _ := m[CBRelationFieldValid].(bool); !valid {
					return false
				}

				ruleId, _ := m[CBRelationFieldRuleID].(string)
				if ruleId != id {
					return false
				}

				saveVer, _ := m[CBRelationFieldRuleVersion].(string)

				return version == VersionForMaster || saveVer == version
			}, values)

		if err != nil {
			return err
		}

		if _, ok := values[id]; ok {
			return nil
		}

		properties := make(map[string]interface{})
		properties[CBFieldNameValid] = false
		properties[CBFieldNameModifyTime] = time.Now()

		key := c.buildKey(id, version)
		if err := updateValue(tx, tblCircuitBreaker, key, properties); err != nil {
			log.Errorf("[Store][circuitBreaker] delete tag rule(%s, %s) err: %s", id, version, err.Error())
			return store.Error(err)
		}

		return nil
	})

	return err
}

// DeleteMasterCircuitBreaker 删除master熔断规则
func (c *circuitBreakerStore) DeleteMasterCircuitBreaker(id string) error {
	return c.DeleteTagCircuitBreaker(id, VersionForMaster)
}

// UpdateCircuitBreaker 修改熔断规则
func (c *circuitBreakerStore) UpdateCircuitBreaker(cb *model.CircuitBreaker) error {
	dbOp := c.handler
	cb.Valid = true
	cb.ModifyTime = time.Now()

	if err := dbOp.SaveValue(tblCircuitBreaker, c.buildKey(cb.ID, cb.Version), cb); err != nil {
		log.Errorf("[Store][CircuitBreaker] update rule(%s,%s) exec err: %s", cb.ID, cb.Version, err.Error())
		return store.Error(err)
	}

	return nil
}

// GetCircuitBreaker 获取熔断规则
func (c *circuitBreakerStore) GetCircuitBreaker(id, version string) (*model.CircuitBreaker, error) {
	var (
		dbOp        = c.handler
		cbKey       = c.buildKey(id, version)
		result, err = dbOp.LoadValues(tblCircuitBreaker, []string{cbKey}, &model.CircuitBreaker{})
	)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get tag rule id(%s) version(%s) err : %s", id, version, err)
		return nil, store.Error(err)
	}

	if len(result) > 1 {
		return nil, fmt.Errorf("[Store][CircuitBreaker] rule(id=%s, version=%s) expect get one, "+
			"but actual more then one, impossible",
			id, version)
	}

	if len(result) == 0 {
		return nil, nil
	}

	cbRet := result[cbKey].(*model.CircuitBreaker)
	if !cbRet.Valid {
		return nil, nil
	}

	return cbRet, nil
}

// GetCircuitBreakerVersions 获取熔断规则的所有版本
func (c *circuitBreakerStore) GetCircuitBreakerVersions(id string) ([]string, error) {
	fields := []string{CBFieldNameID, CBFieldNameValid}
	results, err := c.handler.LoadValuesByFilter(tblCircuitBreaker, fields, &model.CircuitBreaker{},
		func(m map[string]interface{}) bool {
			if valid, _ := m[CBFieldNameValid].(bool); !valid {
				return false
			}

			mV, _ := m[CBFieldNameID].(string)
			return strings.Compare(mV, id) == 0
		})
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get rule_id(%s) links version err : %s", id, err.Error())
		return nil, store.Error(err)
	}

	ans := make([]string, len(results))

	pos := 0
	for _, val := range results {
		record := val.(*model.CircuitBreaker)
		ans[pos] = record.Version
		pos++
	}

	return ans, nil
}

// GetCircuitBreakerMasterRelation 获取熔断规则master版本的绑定关系
func (c *circuitBreakerStore) GetCircuitBreakerMasterRelation(ruleID string) ([]*model.CircuitBreakerRelation, error) {
	return c.GetCircuitBreakerRelation(ruleID, VersionForMaster)
}

// GetCircuitBreakerRelation 获取已标记熔断规则的绑定关系
func (c *circuitBreakerStore) GetCircuitBreakerRelation(
	ruleID, ruleVersion string) ([]*model.CircuitBreakerRelation, error) {
	dbOp := c.handler

	// first: get rule_id => service_ids
	serviceIds, err := c.getServiceIDS(ruleID)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get rule_id(%s) links service_ids err : %s", ruleID, err.Error())
		return nil, store.Error(err)
	}

	// second: batch get relation records
	relations := make([]*model.CircuitBreakerRelation, 0)

	results, err := dbOp.LoadValues(tblCircuitBreakerRelation, serviceIds, &model.CircuitBreakerRelation{})
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get rule_id(%s) relations err : %s", ruleID, err.Error())
		return nil, store.Error(err)
	}

	for _, val := range results {
		record := val.(*model.CircuitBreakerRelation)
		if !record.Valid {
			continue
		}
		if strings.Compare(ruleVersion, record.RuleVersion) != 0 {
			continue
		}
		relations = append(relations, record)
	}

	return relations, nil
}

// GetCircuitBreakerForCache 根据修改时间拉取增量熔断规则
func (c *circuitBreakerStore) GetCircuitBreakerForCache(
	mtime time.Time, firstUpdate bool) ([]*model.ServiceWithCircuitBreaker, error) {
	fields := []string{CBRelationFieldModifyTime}
	relations, err := c.handler.LoadValuesByFilter(tblCircuitBreakerRelation, fields,
		&model.CircuitBreakerRelation{},
		func(m map[string]interface{}) bool {
			mt, _ := m[CBRelationFieldModifyTime].(time.Time)
			isAfter := !mt.Before(mtime)
			return isAfter
		})
	if err != nil {
		return nil, store.Error(err)
	}

	serviceToCbKey := make(map[string]string)
	cbKeys := make([]string, 0)
	for k, v := range relations {
		rel := v.(*model.CircuitBreakerRelation)
		cbKeys = append(cbKeys, c.buildKey(rel.RuleID, rel.RuleVersion))
		serviceToCbKey[k] = c.buildKey(rel.RuleID, rel.RuleVersion)
	}

	cbs, err := c.handler.LoadValues(tblCircuitBreaker, cbKeys, &model.CircuitBreaker{})

	if err != nil {
		return nil, store.Error(err)
	}

	results := make([]*model.ServiceWithCircuitBreaker, 0)
	for serviceId, cbKey := range serviceToCbKey {
		val, ok := cbs[cbKey]
		if !ok {
			log.Error("[Bolt][CircuitBreaker] not exist", zap.String("service-id", serviceId), zap.String("key", cbKey))
			continue
		}

		rule := val.(*model.CircuitBreaker)

		relation := relations[serviceId].(*model.CircuitBreakerRelation)
		results = append(results, &model.ServiceWithCircuitBreaker{
			ServiceID:      serviceId,
			CircuitBreaker: rule,
			Valid:          relation.Valid,
			CreateTime:     relations[serviceId].(*model.CircuitBreakerRelation).CreateTime,
			ModifyTime:     relations[serviceId].(*model.CircuitBreakerRelation).ModifyTime,
		})
	}

	return results, nil
}

// ListMasterCircuitBreakers 获取master熔断规则
func (c *circuitBreakerStore) ListMasterCircuitBreakers(
	filters map[string]string, offset uint32, limit uint32) (*model.CircuitBreakerDetail, error) {
	dbOp := c.handler
	fields := utils.CollectMapKeys(filters)
	fields = append(fields, CBFieldNameVersion, CBFieldNameValid)

	results, err := dbOp.LoadValuesByFilter(tblCircuitBreaker, fields, &model.CircuitBreaker{},
		func(m map[string]interface{}) bool {
			valid, ok := m[CBFieldNameValid]
			if ok && !valid.(bool) {
				return false
			}
			val := m[CBFieldNameVersion].(string)
			if strings.Compare(val, VersionForMaster) != 0 {
				return false
			}
			for k, v := range filters {
				qV, ok := m[k]
				if ok && !reflect.DeepEqual(qV, v) {
					return false
				}
			}
			return true
		})
	if err != nil {
		return nil, store.Error(err)
	}

	// sort paging in memory
	cbSlice := make([]*model.CircuitBreakerInfo, 0)
	for _, v := range results {
		record := v.(*model.CircuitBreaker)
		cbSlice = append(cbSlice, convertCircuitBreakerToInfo(record))
	}

	sort.Slice(cbSlice, func(i, j int) bool {
		a := cbSlice[i]
		b := cbSlice[j]
		return a.CircuitBreaker.ModifyTime.Before(b.CircuitBreaker.ModifyTime)
	})

	// if offset >= len(results), we return all record to client
	if offset >= uint32(len(results)) {
		offset = 0
		limit = math.MaxUint32
	}

	out := &model.CircuitBreakerDetail{
		Total:               uint32(len(results)),
		CircuitBreakerInfos: cbSlice[offset:int(math.Min(float64(offset+limit), float64(len(results))))],
	}

	return out, nil
}

// ListReleaseCircuitBreakers 获取已发布规则
func (c *circuitBreakerStore) ListReleaseCircuitBreakers(
	filters map[string]string, offset, limit uint32) (*model.CircuitBreakerDetail, error) {
	dbOp := c.handler
	emptyCondition := len(filters) == 0

	ruleID, isRuleID := filters["rule_id"]
	ruleVersion, isRuleVer := filters["rule_version"]

	ruleVersions := make(map[string]struct{})
	svcIds := make(map[string][]string)

	fields := []string{CBRelationFieldValid, CBRelationFieldRuleID, CBRelationFieldRuleVersion, CBRelationFieldServiceID}
	retRelations, err := dbOp.LoadValuesByFilter(tblCircuitBreakerRelation, fields, &model.CircuitBreakerRelation{},
		func(m map[string]interface{}) bool {
			if valid, _ := m[CBRelationFieldValid].(bool); !valid {
				return false
			}
			if emptyCondition {
				return true
			}
			ruleIDVal, _ := m[CBRelationFieldRuleID].(string)
			if isRuleID && ruleIDVal != ruleID {
				return false
			}

			ruleVerVal, _ := m[CBRelationFieldRuleVersion].(string)
			if isRuleVer && ruleVerVal != ruleVersion {
				return false
			}
			if _, exist := svcIds[ruleIDVal]; !exist {
				svcIds[ruleIDVal] = make([]string, 0)
			}
			svcIds[ruleIDVal] = append(svcIds[ruleIDVal], m[CBRelationFieldServiceID].(string))
			ruleVersions[ruleVerVal] = struct{}{}
			return true
		})
	if err != nil {
		return nil, store.Error(err)
	}

	fields = []string{CBFieldNameValid, CBFieldNameID, CBFieldNameVersion}
	results, err := dbOp.LoadValuesByFilter(tblCircuitBreaker, fields, &model.CircuitBreaker{},
		func(m map[string]interface{}) bool {
			if valid, _ := m[CBFieldNameValid].(bool); !valid {
				return false
			}
			if isRuleID {
				ruleIDVal := m[CBFieldNameID].(string)
				if ruleIDVal != ruleID {
					return false
				}
			}

			ruleVerVal, _ := m[CBFieldNameVersion].(string)
			if isRuleVer && ruleVersion == ruleVerVal {
				return true
			}
			if _, exist := ruleVersions[ruleVerVal]; !exist {
				return false
			}
			return true
		})
	if err != nil {
		return nil, store.Error(err)
	}

	cbSlice := make([]*model.CircuitBreakerInfo, 0)
	for _, v := range results {
		record := v.(*model.CircuitBreaker)
		svcIds, ok := svcIds[record.ID]
		if ok {
			svcRets, err := dbOp.LoadValues(tblNameService, svcIds, &model.Service{})
			if err != nil {
				return nil, err
			}
			for _, svc := range svcRets {
				cbDetail := convertCircuitBreakerToInfo(record)
				cbDetail.Services = []*model.Service{
					svc.(*model.Service),
				}
				cbSlice = append(cbSlice, cbDetail)
			}
		}
	}

	sort.Slice(cbSlice, func(i, j int) bool {
		a := cbSlice[i]
		b := cbSlice[j]
		return a.CircuitBreaker.ModifyTime.Before(b.CircuitBreaker.ModifyTime)
	})

	if offset >= uint32(len(cbSlice)) {
		return &model.CircuitBreakerDetail{
			Total:               uint32(len(retRelations)),
			CircuitBreakerInfos: []*model.CircuitBreakerInfo{},
		}, nil
	}

	out := &model.CircuitBreakerDetail{
		Total:               uint32(len(retRelations)),
		CircuitBreakerInfos: cbSlice[offset:int(math.Min(float64(offset+limit), float64(len(cbSlice))))],
	}

	return out, nil
}

// GetCircuitBreakersByService 根据服务获取熔断规则
func (c *circuitBreakerStore) GetCircuitBreakersByService(
	name string, namespace string) (*model.CircuitBreaker, error) {
	ss := &serviceStore{
		handler: c.handler,
	}

	service, err := ss.getServiceByNameAndNs(name, namespace)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get service(name=%s, namespace=%s) err : %s", name, namespace, err.Error())
		return nil, store.Error(err)
	}

	if service == nil {
		log.Warnf("[Store][] not found service(namespace=%s, name=%s)", name, namespace)
		return nil, nil
	}

	serviceId := service.ID
	relation, err := c.getCircuitBreakerRelationByServiceId(serviceId)
	if err != nil {
		return nil, store.Error(err)
	}

	if relation == nil {
		log.Warnf("[Store][CircuitBreaker] get release(service-id=%s) not found", serviceId)
		return nil, nil
	}

	return c.GetCircuitBreaker(relation.RuleID, relation.RuleVersion)
}

func (c *circuitBreakerStore) getCircuitBreakerRelationByServiceId(serviceID string) (*model.CircuitBreakerRelation, error) {
	var (
		dbOp        = c.handler
		result, err = dbOp.LoadValues(tblCircuitBreakerRelation, []string{serviceID}, &model.CircuitBreakerRelation{})
	)
	if err != nil {
		return nil, store.Error(err)
	}

	if len(result) == 0 {
		return nil, nil
	}

	if len(result) != 1 {
		return nil, fmt.Errorf("[Store][CircuitBreaker] service_id=%s expect get one, but actual more then one, impossible", serviceID)
	}

	ret := result[serviceID].(*model.CircuitBreakerRelation)
	if !ret.Valid {
		return nil, nil
	}

	return ret, nil
}

func (c *circuitBreakerStore) getServiceIDS(ruleID string) ([]string, error) {
	dbOp := c.handler
	result, err := dbOp.LoadValuesByFilter(tblCircuitBreakerRelation, []string{"RuleID"}, &model.CircuitBreaker{},
		func(m map[string]interface{}) bool {
			id := m["RuleID"].(string)
			return strings.Compare(id, ruleID) == 0
		})
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get tag rule id(%s) err : %s", ruleID, err.Error())
		return nil, store.Error(err)
	}

	if len(result) == 0 {
		return nil, nil
	}

	ids := make([]string, len(result))
	pos := 0
	for serviceId := range result {
		ids[pos] = serviceId
		pos++
	}

	return ids, nil
}

func (c *circuitBreakerStore) buildKey(id, version string) string {
	return fmt.Sprintf("%s_%s", id, version)
}

func (c *circuitBreakerStore) buildMapKey(id string) string {
	return fmt.Sprintf("map_%s", id)
}

func convertCircuitBreakerToInfo(record *model.CircuitBreaker) *model.CircuitBreakerInfo {
	return &model.CircuitBreakerInfo{
		CircuitBreaker: &model.CircuitBreaker{
			ID:         record.ID,
			Version:    record.Version,
			Name:       record.Name,
			Namespace:  record.Namespace,
			Business:   record.Business,
			Department: record.Department,
			Comment:    record.Comment,
			Inbounds:   record.Inbounds,
			Outbounds:  record.Outbounds,
			Token:      record.Token,
			Owner:      record.Owner,
			Revision:   record.Revision,
			CreateTime: record.CreateTime,
			ModifyTime: record.ModifyTime,
		},
		Services: []*model.Service{},
	}
}
