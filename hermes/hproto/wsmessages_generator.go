//go:generate protoc --go-grpc_opt=paths=source_relative --go-grpc_out=. --go_out=. --go_opt=paths=source_relative wsmessages.proto

package hproto
