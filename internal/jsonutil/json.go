package jsonutil

import (
	"crypto/sha256"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
)

const diagnosticRadius = 16

var ErrInvalidMarshalOutput = errors.New("standard JSON marshaler returned invalid output")

type IntegrityError struct {
	ParseError error
	Type       string
	GoVersion  string
	SHA256     string
	Context    string
	ContextHex string
	Length     int
	ByteOffset int64
}

func (e *IntegrityError) Error() string {
	return fmt.Sprintf(
		"%v: type=%s go_version=%s length=%d sha256=%s byte_offset=%d context=%s context_hex=%s parse_error=%v",
		ErrInvalidMarshalOutput,
		e.Type,
		e.GoVersion,
		e.Length,
		e.SHA256,
		e.ByteOffset,
		e.Context,
		e.ContextHex,
		e.ParseError,
	)
}

func (e *IntegrityError) Unwrap() error {
	return ErrInvalidMarshalOutput
}

func Marshal(v any) ([]byte, error) {
	return marshalWith(func(v any) ([]byte, error) {
		return json.Marshal(v)
	}, v)
}

func marshalWith(marshal func(any) ([]byte, error), v any) ([]byte, error) {
	data, err := marshal(v)
	if err != nil {
		return nil, err
	}
	if jsontext.Value(data).IsValid() {
		return data, nil
	}
	return nil, newIntegrityError(v, data)
}

func newIntegrityError(v any, data []byte) *IntegrityError {
	var decoded any
	parseErr := json.Unmarshal(data, &decoded)
	offset := int64(-1)
	if syntaxErr, ok := errors.AsType[*jsontext.SyntacticError](parseErr); ok {
		offset = syntaxErr.ByteOffset
	}

	start, end := diagnosticBounds(len(data), offset)
	digest := sha256.Sum256(data)
	typeName := "<nil>"
	if typ := reflect.TypeOf(v); typ != nil {
		typeName = typ.String()
	}

	return &IntegrityError{
		Type:       typeName,
		GoVersion:  runtime.Version(),
		Length:     len(data),
		SHA256:     fmt.Sprintf("%x", digest),
		ByteOffset: offset,
		Context:    strconv.QuoteToASCII(string(data[start:end])),
		ContextHex: fmt.Sprintf("%x", data[start:end]),
		ParseError: parseErr,
	}
}

func diagnosticBounds(length int, offset int64) (int, int) {
	center := int(offset)
	if offset < 0 || offset > int64(length) {
		center = 0
	}
	start := max(0, center-diagnosticRadius)
	end := min(length, center+diagnosticRadius+1)
	return start, end
}

func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
