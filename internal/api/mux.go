package api

import (
	"net/http"
	"strings"

	"google.golang.org/grpc"
)

// Combine serves gRPC and REST on one listener: HTTP/2 requests with a
// gRPC content type go to the gRPC server, everything else to rest.
func Combine(grpcSrv *grpc.Server, rest http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcSrv.ServeHTTP(w, r)
			return
		}
		rest.ServeHTTP(w, r)
	})
}
