// Copyright 2020, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by protoc-gen-connect-go. DO NOT EDIT.
//
// Source: opentelemetry/proto/collector/logs/v1/logs_service.proto

package logsv1connect

import (
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	context "context"
	errors "errors"
	connect_go "github.com/bufbuild/connect-go"
	http "net/http"
	strings "strings"
)

// This is a compile-time assertion to ensure that this generated file and the connect package are
// compatible. If you get a compiler error that this constant is not defined, this code was
// generated with a version of connect newer than the one compiled into your binary. You can fix the
// problem by either regenerating this code with an older version of connect or updating the connect
// version compiled into your binary.
const _ = connect_go.IsAtLeastVersion0_1_0

const (
	// LogsServiceName is the fully-qualified name of the LogsService service.
	LogsServiceName = "opentelemetry.proto.collector.logs.v1.LogsService"
)

// These constants are the fully-qualified names of the RPCs defined in this package. They're
// exposed at runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// LogsServiceExportProcedure is the fully-qualified name of the LogsService's Export RPC.
	LogsServiceExportProcedure = "/opentelemetry.proto.collector.logs.v1.LogsService/Export"
)

// LogsServiceClient is a client for the opentelemetry.proto.collector.logs.v1.LogsService service.
type LogsServiceClient interface {
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(context.Context, *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error)
}

// NewLogsServiceClient constructs a client for the
// opentelemetry.proto.collector.logs.v1.LogsService service. By default, it uses the Connect
// protocol with the binary Protobuf Codec, asks for gzipped responses, and sends uncompressed
// requests. To use the gRPC or gRPC-Web protocols, supply the connect.WithGRPC() or
// connect.WithGRPCWeb() options.
//
// The URL supplied here should be the base URL for the Connect or gRPC server (for example,
// http://api.acme.com or https://acme.com/grpc).
func NewLogsServiceClient(httpClient connect_go.HTTPClient, baseURL string, opts ...connect_go.ClientOption) LogsServiceClient {
	baseURL = strings.TrimRight(baseURL, "/")
	return &logsServiceClient{
		export: connect_go.NewClient[v1.ExportLogsServiceRequest, v1.ExportLogsServiceResponse](
			httpClient,
			baseURL+LogsServiceExportProcedure,
			opts...,
		),
	}
}

// logsServiceClient implements LogsServiceClient.
type logsServiceClient struct {
	export *connect_go.Client[v1.ExportLogsServiceRequest, v1.ExportLogsServiceResponse]
}

// Export calls opentelemetry.proto.collector.logs.v1.LogsService.Export.
func (c *logsServiceClient) Export(ctx context.Context, req *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error) {
	return c.export.CallUnary(ctx, req)
}

// LogsServiceHandler is an implementation of the opentelemetry.proto.collector.logs.v1.LogsService
// service.
type LogsServiceHandler interface {
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(context.Context, *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error)
}

// NewLogsServiceHandler builds an HTTP handler from the service implementation. It returns the path
// on which to mount the handler and the handler itself.
//
// By default, handlers support the Connect, gRPC, and gRPC-Web protocols with the binary Protobuf
// and JSON codecs. They also support gzip compression.
func NewLogsServiceHandler(svc LogsServiceHandler, opts ...connect_go.HandlerOption) (string, http.Handler) {
	logsServiceExportHandler := connect_go.NewUnaryHandler(
		LogsServiceExportProcedure,
		svc.Export,
		opts...,
	)
	return "/opentelemetry.proto.collector.logs.v1.LogsService/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case LogsServiceExportProcedure:
			logsServiceExportHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// UnimplementedLogsServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedLogsServiceHandler struct{}

func (UnimplementedLogsServiceHandler) Export(context.Context, *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error) {
	return nil, connect_go.NewError(connect_go.CodeUnimplemented, errors.New("opentelemetry.proto.collector.logs.v1.LogsService.Export is not implemented"))
}
