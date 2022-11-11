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

package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"

	"github.com/polarismesh/polaris/apiserver/httpserver/i18n"
	api "github.com/polarismesh/polaris/common/api/v1"
	apiv2 "github.com/polarismesh/polaris/common/api/v2"
	"github.com/polarismesh/polaris/common/log"
	"github.com/polarismesh/polaris/common/utils"
)

// Handler HTTP请求/回复处理器
type Handler struct {
	Request  *restful.Request
	Response *restful.Response
}

// ParseArray 解析PB数组对象
func (h *Handler) ParseArray(createMessage func() proto.Message) (context.Context, error) {
	requestID := h.Request.HeaderParameter("Request-Id")

	jsonDecoder := json.NewDecoder(h.Request.Request.Body)
	// read open bracket
	_, err := jsonDecoder.Token()
	if err != nil {
		log.Error(err.Error(), utils.ZapRequestID(requestID))
		return nil, err
	}
	for jsonDecoder.More() {
		protoMessage := createMessage()
		err := jsonpb.UnmarshalNext(jsonDecoder, protoMessage)
		if err != nil {
			log.Error(err.Error(), utils.ZapRequestID(requestID))
			return nil, err
		}
	}
	return h.postParseMessage(requestID)
}

func (h *Handler) postParseMessage(requestID string) (context.Context, error) {
	platformID := h.Request.HeaderParameter("Platform-Id")
	platformToken := h.Request.HeaderParameter("Platform-Token")
	token := h.Request.HeaderParameter("Polaris-Token")
	authToken := h.Request.HeaderParameter(utils.HeaderAuthTokenKey)
	ctx := context.Background()
	ctx = context.WithValue(ctx, utils.StringContext("request-id"), requestID)
	ctx = context.WithValue(ctx, utils.StringContext("platform-id"), platformID)
	ctx = context.WithValue(ctx, utils.StringContext("platform-token"), platformToken)
	if token != "" {
		ctx = context.WithValue(ctx, utils.StringContext("polaris-token"), token)
	}
	if authToken != "" {
		ctx = context.WithValue(ctx, utils.ContextAuthTokenKey, authToken)
	}

	var operator string
	addrSlice := strings.Split(h.Request.Request.RemoteAddr, ":")
	if len(addrSlice) == 2 {
		operator = "HTTP:" + addrSlice[0]
		if platformID != "" {
			operator += "(" + platformID + ")"
		}
	}
	if staffName := h.Request.HeaderParameter("Staffname"); staffName != "" {
		operator = staffName
	}
	ctx = context.WithValue(ctx, utils.StringContext("operator"), operator)

	return ctx, nil
}

// Parse 解析请求
func (h *Handler) Parse(message proto.Message) (context.Context, error) {
	requestID := h.Request.HeaderParameter("Request-Id")
	if err := jsonpb.Unmarshal(h.Request.Request.Body, message); err != nil {
		log.Error(err.Error(), utils.ZapRequestID(requestID))
		return nil, err
	}
	return h.postParseMessage(requestID)
}

// ParseHeaderContext 将http请求header中携带的用户信息提取出来
func (h *Handler) ParseHeaderContext() context.Context {
	requestID := h.Request.HeaderParameter("Request-Id")
	platformID := h.Request.HeaderParameter("Platform-Id")
	platformToken := h.Request.HeaderParameter("Platform-Token")
	token := h.Request.HeaderParameter("Polaris-Token")
	authToken := h.Request.HeaderParameter(utils.HeaderAuthTokenKey)

	ctx := context.Background()
	ctx = context.WithValue(ctx, utils.StringContext("request-id"), requestID)
	ctx = context.WithValue(ctx, utils.StringContext("platform-id"), platformID)
	ctx = context.WithValue(ctx, utils.StringContext("platform-token"), platformToken)
	ctx = context.WithValue(ctx, utils.ContextClientAddress, h.Request.Request.RemoteAddr)
	if token != "" {
		ctx = context.WithValue(ctx, utils.StringContext("polaris-token"), token)
	}
	if authToken != "" {
		ctx = context.WithValue(ctx, utils.ContextAuthTokenKey, authToken)
	}

	var operator string
	addrSlice := strings.Split(h.Request.Request.RemoteAddr, ":")
	if len(addrSlice) == 2 {
		operator = "HTTP:" + addrSlice[0]
		if platformID != "" {
			operator += "(" + platformID + ")"
		}
	}
	if staffName := h.Request.HeaderParameter("Staffname"); staffName != "" {
		operator = staffName
	}
	ctx = context.WithValue(ctx, utils.StringContext("operator"), operator)

	return ctx
}

// WriteHeader 仅返回Code
func (h *Handler) WriteHeader(polarisCode uint32, httpStatus int) {
	requestID := h.Request.HeaderParameter(utils.PolarisRequestID)
	h.Request.SetAttribute(utils.PolarisCode, polarisCode) // api统计的时候，用该code

	// 对于非200000的返回，补充实际的code到header中
	if polarisCode != api.ExecuteSuccess {
		h.Response.AddHeader(utils.PolarisCode, fmt.Sprintf("%d", polarisCode))
		h.Response.AddHeader(utils.PolarisMessage, api.Code2Info(polarisCode))
	}
	h.Response.AddHeader("Request-Id", requestID)
	h.Response.WriteHeader(httpStatus)
}

// WriteHeaderAndProto 返回Code和Proto
func (h *Handler) WriteHeaderAndProto(obj api.ResponseMessage) {
	requestID := h.Request.HeaderParameter(utils.PolarisRequestID)
	h.Request.SetAttribute(utils.PolarisCode, obj.GetCode().GetValue())
	status := api.CalcCode(obj)

	if status != http.StatusOK {
		log.Error(obj.String(), utils.ZapRequestID(requestID))
	}
	if code := obj.GetCode().GetValue(); code != api.ExecuteSuccess {
		h.Response.AddHeader(utils.PolarisCode, fmt.Sprintf("%d", code))
		h.Response.AddHeader(utils.PolarisMessage, api.Code2Info(code))
	}
	h.Response.AddHeader(utils.PolarisRequestID, requestID)
	h.Response.WriteHeader(status)

	m := jsonpb.Marshaler{Indent: " ", EmitDefaults: true}
	err := m.Marshal(h.Response, h.i18nAction(obj))
	if err != nil {
		log.Error(err.Error(), utils.ZapRequestID(requestID))
	}
}

// WriteHeaderAndProtoV2 返回Code和Proto
func (h *Handler) WriteHeaderAndProtoV2(obj apiv2.ResponseMessage) {
	requestID := h.Request.HeaderParameter(utils.PolarisRequestID)
	h.Request.SetAttribute(utils.PolarisCode, obj.GetCode())
	status := apiv2.CalcCode(obj)

	if status != http.StatusOK {
		log.Error(obj.String(), utils.ZapRequestID(requestID))
	}
	if code := obj.GetCode(); code != api.ExecuteSuccess {
		h.Response.AddHeader(utils.PolarisCode, fmt.Sprintf("%d", code))
		h.Response.AddHeader(utils.PolarisMessage, api.Code2Info(code))
	}

	h.Response.AddHeader(utils.PolarisRequestID, requestID)
	h.Response.WriteHeader(status)

	m := jsonpb.Marshaler{Indent: " ", EmitDefaults: true}
	err := m.Marshal(h.Response, obj)
	if err != nil {
		log.Error(err.Error(), utils.ZapRequestID(requestID))
	}
}

// HTTPResponse http答复简单封装
func HTTPResponse(req *restful.Request, rsp *restful.Response, code uint32) {
	handler := &Handler{
		Request:  req,
		Response: rsp,
	}
	resp := api.NewResponse(code)
	handler.WriteHeaderAndProto(resp)
}

// i18nAction 依据resp.code进行国际化resp.info信息
// 当与header中的信息不匹配时, 则使用原文, 后续通过新定义code的方式增量解决
// 当header的msg 与 resp.info一致时, 根据resp.code国际化信息
func (h *Handler) i18nAction(obj api.ResponseMessage) api.ResponseMessage {
	hMsg := h.Response.Header().Get(utils.PolarisMessage)
	info := obj.GetInfo()
	if hMsg != info.GetValue() {
		return obj
	}
	code := obj.GetCode()
	msg, err := i18n.Translate(
		code.GetValue(), h.Request.QueryParameter("lang"), h.Request.HeaderParameter("Accept-Language"))
	if msg == "" || err != nil {
		return obj
	}
	*info = wrappers.StringValue{Value: msg}
	return obj
}

// ParseQueryParams 解析并获取HTTP的query params
func ParseQueryParams(req *restful.Request) map[string]string {
	queryParams := make(map[string]string)
	for key, value := range req.Request.URL.Query() {
		if len(value) > 0 {
			queryParams[key] = value[0] // 暂时默认只支持一个查询
		}
	}

	return queryParams
}

// ParseJsonBody parse http body as json object
func ParseJsonBody(req *restful.Request, value interface{}) error {
	body, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, value); err != nil {
		return err
	}
	return nil
}
