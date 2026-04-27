// Package grpc provides gRPC capture — intercepts gRPC calls and normalises
// them into sentinel.packet.v1 governance packets.
//
// Uses gRPC UnaryInterceptor and StreamInterceptor. The interceptors extract
// the method name, actor identity (from metadata), and payload hash.
//
// M5 implementation milestone.
package grpc
