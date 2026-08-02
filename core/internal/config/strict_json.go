package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

func validateJSONDocument(data []byte, schema any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, reflect.TypeOf(schema), "$"); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("config file contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing config data: %w", err)
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder, schema reflect.Type, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if token == nil {
		return fmt.Errorf("null is not allowed at %s", path)
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		return validateJSONObject(decoder, schema, path)
	case '[':
		return validateJSONArray(decoder, schema, path)
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, path)
	}
}

func validateJSONObject(decoder *json.Decoder, schema reflect.Type, path string) error {
	fields := jsonFieldTypes(schema)
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode object key at %s: %w", path, err)
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("object key at %s must be a string", path)
		}

		fieldPath := jsonFieldPath(path, name)
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate field %q at %s", name, fieldPath)
		}
		seen[name] = struct{}{}

		fieldType := reflect.Type(nil)
		if fields != nil {
			var exists bool
			fieldType, exists = fields[name]
			if !exists {
				return fmt.Errorf("unknown field %q at %s", name, fieldPath)
			}
		}
		if err := validateJSONValue(decoder, fieldType, fieldPath); err != nil {
			return err
		}
	}

	if token, err := decoder.Token(); err != nil {
		return fmt.Errorf("close object at %s: %w", path, err)
	} else if token != json.Delim('}') {
		return fmt.Errorf("unexpected JSON token %v at %s", token, path)
	}
	return nil
}

func validateJSONArray(decoder *json.Decoder, schema reflect.Type, path string) error {
	elementType := jsonElementType(schema)
	index := 0
	for decoder.More() {
		if err := validateJSONValue(decoder, elementType, fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
		index++
	}

	if token, err := decoder.Token(); err != nil {
		return fmt.Errorf("close array at %s: %w", path, err)
	} else if token != json.Delim(']') {
		return fmt.Errorf("unexpected JSON token %v at %s", token, path)
	}
	return nil
}

func jsonFieldTypes(schema reflect.Type) map[string]reflect.Type {
	schema = dereferenceType(schema)
	if schema == nil || schema.Kind() != reflect.Struct {
		return nil
	}

	fields := make(map[string]reflect.Type)
	for index := 0; index < schema.NumField(); index++ {
		field := schema.Field(index)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func jsonElementType(schema reflect.Type) reflect.Type {
	schema = dereferenceType(schema)
	if schema == nil || (schema.Kind() != reflect.Array && schema.Kind() != reflect.Slice) {
		return nil
	}
	return schema.Elem()
}

func dereferenceType(schema reflect.Type) reflect.Type {
	for schema != nil && schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	return schema
}

func jsonFieldPath(parent, field string) string {
	if field == "" {
		return parent + "[\"\"]"
	}
	return parent + "." + field
}
