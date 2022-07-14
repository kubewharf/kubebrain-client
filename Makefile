

# generate rpc format proto
rpc_proto:
	protoc  -I ./  -I $$GOPATH/src/  -I $$GOPATH/src/github.com/gogo/googleapis/ \
	--gogo_out=plugins=grpc,\
	Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,\
	Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,\
	Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types,\
	Mgoogle/api/annotations.proto=github.com/gogo/googleapis/google/api,\
	Mgoogle/protobuf/field_mask.proto=github.com/gogo/protobuf/types:$$GOPATH/src \
	--grpc-gateway_out=\
	Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,\
	Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,\
	Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types,\
	Mgoogle/api/annotations.proto=github.com/gogo/googleapis/google/api,\
	Mgoogle/protobuf/field_mask.proto=github.com/gogo/protobuf/types:$$GOPATH/src \
	api/v2rpc/*.proto
