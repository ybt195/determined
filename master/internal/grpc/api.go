package grpc

import (
	"context"
	"fmt"
	"net"

	grpcmiddleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpclogrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpcrecovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	proto "github.com/determined-ai/determined/master/pkg/proto/apiv1"
)

const jsonPretty = "application/json+pretty"

// StartGRPCServer starts the Determined gRPC service and the given port.
func StartGRPCServer(srv proto.DeterminedServer, port int) error {
	addr := fmt.Sprintf(":%d", port)
	logger := logrus.NewEntry(logrus.StandardLogger())
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrapf(err, "grpc server could not bind: %s", addr)
	}
	opts := []grpclogrus.Option{
		grpclogrus.WithLevels(grpcCodeToLogrusLevel),
	}
	grpclogrus.ReplaceGrpcLogger(logger)
	grpcS := grpc.NewServer(
		grpc.StreamInterceptor(grpcmiddleware.ChainStreamServer(
			grpclogrus.StreamServerInterceptor(logger, opts...),
			grpcrecovery.StreamServerInterceptor(),
		)),
		grpc.UnaryInterceptor(grpcmiddleware.ChainUnaryServer(
			grpclogrus.UnaryServerInterceptor(logger, opts...),
			grpcrecovery.UnaryServerInterceptor(),
		)),
	)
	proto.RegisterDeterminedServer(grpcS, srv)
	return grpcS.Serve(l)
}

// RegisterHTTPProxy registers grpc-gateway with the master echo server.
func RegisterHTTPProxy(e *echo.Echo, port int) error {
	addr := fmt.Sprintf(":%d", port)
	serverOpts := []runtime.ServeMuxOption{
		runtime.WithMarshalerOption(jsonPretty,
			&runtime.JSONPb{EmitDefaults: true, Indent: "    "}),
		runtime.WithMarshalerOption(runtime.MIMEWildcard,
			&runtime.JSONPb{EmitDefaults: true}),
		runtime.WithProtoErrorHandler(errorHandler),
	}
	mux := runtime.NewServeMux(serverOpts...)
	opts := []grpc.DialOption{grpc.WithInsecure()}
	err := proto.RegisterDeterminedHandlerFromEndpoint(context.Background(), mux, addr, opts)
	if err != nil {
		return err
	}
	e.Any("/api/v1/*", func(c echo.Context) error {
		request := c.Request()
		if _, ok := request.URL.Query()["pretty"]; ok {
			request.Header.Set("Accept", jsonPretty)
		}
		mux.ServeHTTP(c.Response(), request)
		return nil
	}, middleware.RemoveTrailingSlash())
	return nil
}
