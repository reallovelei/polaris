//go:build integration
// +build integration

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

package test

import (
	"testing"
	"time"

	"github.com/ArthurHlt/go-eureka-client/eureka"
)

// TestEurekaServer_RegisterApplication 测试EurekaServer大小写
func TestEurekaServer_RegisterApplication(t *testing.T) {
	client := eureka.NewClient([]string{
		"http://127.0.0.1:8761/eureka", //From a spring boot based eureka server
	})
	appId := "testAPP"
	instance := eureka.NewInstanceInfo("TEST", appId, "69.172.200.23", 80, 30, false) //Create a new instance to register
	instance.Metadata = &eureka.MetaData{
		Map: make(map[string]string),
	}
	instance.Metadata.Map["foo"] = "bar" //add metadata for example
	var err error
	err = client.RegisterInstance(appId, instance) // Register new instance in your eureka(s)
	if err != nil {
		t.Fatal(err)
	}

	applications, _ := client.GetApplications() // Retrieves all applications from eureka server(s)
	t.Log(applications)

	time.Sleep(time.Second)
	_, err = client.GetApplication(appId)
	if err != nil {
		t.Error(err)
	}
}
