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

package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/emicklei/go-restful/v3"

	httpcommon "github.com/polarismesh/polaris/apiserver/httpserver/http"
	api "github.com/polarismesh/polaris/common/api/v1"
	"github.com/polarismesh/polaris/common/utils"
	"github.com/polarismesh/polaris/maintain"
)

// GetMaintainAccessServer 运维接口
func (h *HTTPServer) GetMaintainAccessServer() *restful.WebService {
	ws := new(restful.WebService)
	ws.Path("/maintain/v1").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)

	ws.Route(enrichGetServerConnectionsApiDocs(ws.GET("/apiserver/conn").To(h.GetServerConnections)))
	ws.Route(enrichGetServerConnStatsApiDocs(ws.GET("/apiserver/conn/stats").To(h.GetServerConnStats)))
	ws.Route(enrichCloseConnectionsApiDocs(ws.POST("apiserver/conn/close").To(h.CloseConnections)))
	ws.Route(enrichFreeOSMemoryApiDocs(ws.POST("/memory/free").To(h.FreeOSMemory)))
	ws.Route(enrichCleanInstanceApiDocs(ws.POST("/instance/clean").To(h.CleanInstance)))
	ws.Route(enrichBatchCleanInstancesApiDocs(ws.POST("/instance/batchclean").To(h.BatchCleanInstances)))
	ws.Route(enrichGetLastHeartbeatApiDocs(ws.GET("/instance/heartbeat").To(h.GetLastHeartbeat)))
	ws.Route(enrichGetLogOutputLevelApiDocs(ws.GET("/log/outputlevel").To(h.GetLogOutputLevel)))
	ws.Route(enrichSetLogOutputLevelApiDocs(ws.PUT("/log/outputlevel").To(h.SetLogOutputLevel)))
	return ws
}

// GetServerConnections 查看server的连接数
// query参数：protocol，必须，查看指定协议server
//
//	host，可选，查看指定host
func (h *HTTPServer) GetServerConnections(req *restful.Request, rsp *restful.Response) {
	ctx := initContext(req)
	params := httpcommon.ParseQueryParams(req)
	connReq := maintain.ConnReq{
		Protocol: params["protocol"],
		Host:     params["host"],
	}

	ret, err := h.maintainServer.GetServerConnections(ctx, &connReq)
	if err != nil {
		_ = rsp.WriteError(http.StatusBadRequest, err)
	} else {
		_ = rsp.WriteEntity(ret)
	}
}

// GetServerConnStats 获取连接缓存里面的统计信息
func (h *HTTPServer) GetServerConnStats(req *restful.Request, rsp *restful.Response) {
	ctx := initContext(req)
	params := httpcommon.ParseQueryParams(req)

	var amount int
	if amountStr, ok := params["amount"]; ok {
		if n, err := strconv.Atoi(amountStr); err == nil {
			amount = n
		}
	}

	connReq := maintain.ConnReq{
		Protocol: params["protocol"],
		Host:     params["host"],
		Amount:   amount,
	}

	ret, err := h.maintainServer.GetServerConnStats(ctx, &connReq)
	if err != nil {
		_ = rsp.WriteError(http.StatusBadRequest, err)
	} else {
		_ = rsp.WriteAsJson(ret)
	}
}

// CloseConnections 关闭指定client ip的连接
func (h *HTTPServer) CloseConnections(req *restful.Request, rsp *restful.Response) {
	log.Info("[MAINTAIN] Start doing close connections")
	ctx := initContext(req)
	var connReqs []maintain.ConnReq
	decoder := json.NewDecoder(req.Request.Body)
	if err := decoder.Decode(&connReqs); err != nil {
		log.Errorf("[MAINTAIN] close connection decode body err: %s", err.Error())
		_ = rsp.WriteError(http.StatusBadRequest, err)
		return
	}
	for _, entry := range connReqs {
		if entry.Protocol == "" {
			log.Errorf("[MAINTAIN] close connection missing protocol")
			_ = rsp.WriteErrorString(http.StatusBadRequest, "missing protocol")
			return
		}
		if entry.Host == "" {
			log.Errorf("[MAINTAIN] close connection missing host")
			_ = rsp.WriteErrorString(http.StatusBadRequest, "missing host")
			return
		}
	}

	if err := h.maintainServer.CloseConnections(ctx, connReqs); err != nil {
		_ = rsp.WriteError(http.StatusBadRequest, err)
	}
}

// FreeOSMemory 增加一个释放系统内存的接口
func (h *HTTPServer) FreeOSMemory(req *restful.Request, rsp *restful.Response) {
	ctx := initContext(req)
	if err := h.maintainServer.FreeOSMemory(ctx); err != nil {
		_ = rsp.WriteError(http.StatusBadRequest, err)
	}
}

// CleanInstance 彻底清理flag=1的实例运维接口
// 支持一个个清理
func (h *HTTPServer) CleanInstance(req *restful.Request, rsp *restful.Response) {
	handler := &httpcommon.Handler{
		Request:  req,
		Response: rsp,
	}

	instance := &api.Instance{}
	ctx, err := handler.Parse(instance)
	if err != nil {
		handler.WriteHeaderAndProto(api.NewResponseWithMsg(api.ParseException, err.Error()))
		return
	}

	handler.WriteHeaderAndProto(h.maintainServer.CleanInstance(ctx, instance))
}

func (h *HTTPServer) BatchCleanInstances(req *restful.Request, rsp *restful.Response) {
	ctx := initContext(req)

	var param struct {
		BatchSize uint32 `json:"batch_size"`
	}

	if err := httpcommon.ParseJsonBody(req, &param); err != nil {
		_ = rsp.WriteError(http.StatusBadRequest, err)
		return
	}

	if count, err := h.maintainServer.BatchCleanInstances(ctx, param.BatchSize); err != nil {
		_ = rsp.WriteError(http.StatusInternalServerError, err)
	} else {
		var ret struct {
			RowsAffected uint32 `json:"rows_affected"`
		}
		ret.RowsAffected = count
		_ = rsp.WriteAsJson(ret)
	}

}

// GetLastHeartbeat 获取实例，上一次心跳的时间
func (h *HTTPServer) GetLastHeartbeat(req *restful.Request, rsp *restful.Response) {
	ctx := initContext(req)
	handler := &httpcommon.Handler{
		Request:  req,
		Response: rsp,
	}
	params := httpcommon.ParseQueryParams(req)
	instance := &api.Instance{}
	if id, ok := params["id"]; ok && id != "" {
		instance.Id = utils.NewStringValue(id)
	} else {
		instance.Service = utils.NewStringValue(params["service"])
		instance.Namespace = utils.NewStringValue(params["namespace"])
		instance.VpcId = utils.NewStringValue(params["vpc_id"])
		instance.Host = utils.NewStringValue(params["host"])
		port, _ := strconv.Atoi(params["port"])
		instance.Port = utils.NewUInt32Value(uint32(port))
	}

	ret := h.maintainServer.GetLastHeartbeat(ctx, instance)
	handler.WriteHeaderAndProto(ret)
}

// GetLogOutputLevel 获取日志输出级别
func (h *HTTPServer) GetLogOutputLevel(req *restful.Request, rsp *restful.Response) {
	ctx := initContext(req)

	out, err := h.maintainServer.GetLogOutputLevel(ctx)
	if err != nil {
		_ = rsp.WriteError(http.StatusBadRequest, err)
	} else {
		_ = rsp.WriteAsJson(out)
	}
}

// SetLogOutputLevel 设置日志输出级别
func (h *HTTPServer) SetLogOutputLevel(req *restful.Request, rsp *restful.Response) {
	ctx := initContext(req)

	var scopeLogLevel struct {
		Scope string `json:"scope"`
		Level string `json:"level"`
	}

	if err := httpcommon.ParseJsonBody(req, &scopeLogLevel); err != nil {
		_ = rsp.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}

	if err := h.maintainServer.SetLogOutputLevel(ctx, scopeLogLevel.Scope, scopeLogLevel.Level); err != nil {
		_ = rsp.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}

	_ = rsp.WriteEntity("ok")
}

func initContext(req *restful.Request) context.Context {
	ctx := context.Background()

	authToken := req.HeaderParameter(utils.HeaderAuthTokenKey)
	if authToken != "" {
		ctx = context.WithValue(ctx, utils.ContextAuthTokenKey, authToken)
	}

	return ctx
}
