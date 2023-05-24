

# generate rpc format proto
protoc -I=. -I=$GOPATH/src -I=$GOPATH/src/github.com/gogo/googleapis/ --gogo_out=plugins=grpc,\
    Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,\
    Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,\
    Mgoogle/protobuf/struct.proto=github.com/gogo/protobuf/types,\
    Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,\
    Mgoogle/api/annotations.proto=github.com/gogo/googleapis/google/api,\
    Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types:. \
    api/v2rpc/*.proto