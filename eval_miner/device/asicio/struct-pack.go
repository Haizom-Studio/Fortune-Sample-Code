package asicio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
)

func Pack(elts ...interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := packType(buf, elts...); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func packValue(buf io.Writer, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return fmt.Errorf("cannot pack nil %s", v.Type().String())
		}
		return packValue(buf, v.Elem())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if err := packValue(buf, f); err != nil {
				return err
			}
		}
	default:
		return binary.Write(buf, binary.LittleEndian, v.Interface())
	}
	return nil
}

func packType(buf io.Writer, elts ...interface{}) error {
	for _, e := range elts {
		if err := packValue(buf, reflect.ValueOf(e)); err != nil {
			return err
		}
	}

	return nil
}

// Unpack is a convenience wrapper around UnpackBuf. Unpack returns the number
// of bytes read from b to fill elts and error, if any.
func Unpack(b []byte, elts ...interface{}) (int, error) {
	buf := bytes.NewBuffer(b)
	err := UnpackBuf(buf, elts...)
	read := len(b) - buf.Len()
	return read, err
}

func unpackValue(buf io.Reader, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return fmt.Errorf("cannot unpack nil %s", v.Type().String())
		}
		return unpackValue(buf, v.Elem())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if err := unpackValue(buf, f); err != nil {
				return err
			}
		}
		return nil
	default:
		// binary.Read can only set pointer values, so we need to take the address.
		if !v.CanAddr() {
			return fmt.Errorf("cannot unpack unaddressable leaf type %q", v.Type().String())
		}
		return binary.Read(buf, binary.LittleEndian, v.Addr().Interface())
	}
}

func UnpackBuf(buf io.Reader, elts ...interface{}) error {
	for _, e := range elts {
		v := reflect.ValueOf(e)
		if v.Kind() != reflect.Ptr {
			return fmt.Errorf("non-pointer value %q passed to UnpackBuf", v.Type().String())
		}
		if v.IsNil() {
			return errors.New("nil pointer passed to UnpackBuf")
		}

		if err := unpackValue(buf, v); err != nil {
			return err
		}
	}
	return nil
}
