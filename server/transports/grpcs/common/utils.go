package common

import (
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"google.golang.org/protobuf/types/known/structpb"
)

func ErrString(err error) string {
	if err == nil {
		return ""
	}
	return redaction.RedactError(err)
}

func DecodeStruct(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}
