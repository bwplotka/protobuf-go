// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package protocmp provides protobuf specific options for the cmp package.
//
// The primary feature is the Transform option, which transform proto.Message
// types into a Message map that is suitable for cmp to introspect upon.
// All other options in this package must be used in conjunction with Transform.
package protocmp

import (
	"reflect"
	"strconv"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/protobuf/internal/encoding/wire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/runtime/protoiface"
	"google.golang.org/protobuf/runtime/protoimpl"
)

var (
	messageV1Type = reflect.TypeOf((*protoiface.MessageV1)(nil)).Elem()
	messageV2Type = reflect.TypeOf((*proto.Message)(nil)).Elem()
)

// Enum is a dynamic representation of a protocol buffer enum that is
// suitable for cmp.Equal and cmp.Diff to compare upon.
type Enum struct {
	num protoreflect.EnumNumber
	ed  protoreflect.EnumDescriptor
}

// Descriptor returns the enum descriptor.
// It returns nil for a zero Enum value.
func (e Enum) Descriptor() protoreflect.EnumDescriptor {
	return e.ed
}

// Number returns the enum value as an integer.
func (e Enum) Number() protoreflect.EnumNumber {
	return e.num
}

// Equal reports whether e1 and e2 represent the same enum value.
func (e1 Enum) Equal(e2 Enum) bool {
	if e1.ed.FullName() != e2.ed.FullName() {
		return false
	}
	return e1.num == e2.num
}

// String returns the name of the enum value if known (e.g., "ENUM_VALUE"),
// otherwise it returns the formatted decimal enum number (e.g., "14").
func (e Enum) String() string {
	if ev := e.ed.Values().ByNumber(e.num); ev != nil {
		return string(ev.Name())
	}
	return strconv.Itoa(int(e.num))
}

const messageTypeKey = "@type"

type messageType struct {
	md  protoreflect.MessageDescriptor
	xds map[protoreflect.FullName]protoreflect.ExtensionDescriptor
}

func (t messageType) String() string {
	return string(t.md.FullName())
}

func (t1 messageType) Equal(t2 messageType) bool {
	return t1.md.FullName() == t2.md.FullName()
}

// Message is a dynamic representation of a protocol buffer message that is
// suitable for cmp.Equal and cmp.Diff to directly operate upon.
//
// Every populated known field (excluding extension fields) is stored in the map
// with the key being the short name of the field (e.g., "field_name") and
// the value determined by the kind and cardinality of the field.
//
// Singular scalars are represented by the same Go type as protoreflect.Value,
// singular messages are represented by the Message type,
// singular enums are represented by the Enum type,
// list fields are represented as a Go slice, and
// map fields are represented as a Go map.
//
// Every populated extension field is stored in the map with the key being the
// full name of the field surrounded by brackets (e.g., "[extension.full.name]")
// and the value determined according to the same rules as known fields.
//
// Every unknown field is stored in the map with the key being the field number
// encoded as a decimal string (e.g., "132") and the value being the raw bytes
// of the encoded field (as the protoreflect.RawFields type).
//
// Message values must not be created by or mutated by users.
type Message map[string]interface{}

// Descriptor return the message descriptor.
// It returns nil for a zero Message value.
func (m Message) Descriptor() protoreflect.MessageDescriptor {
	mt, _ := m[messageTypeKey].(messageType)
	return mt.md
}

// TODO: There is currently no public API for retrieving the FieldDescriptors
// for extension fields. Rather than adding a specialized API to support that,
// perhaps Message should just implement protoreflect.ProtoMessage instead.

// String returns a formatted string for the message.
// It is intended for human debugging and has no guarantees about its
// exact format or the stability of its output.
func (m Message) String() string {
	if m == nil {
		return "<nil>"
	}
	return string(appendMessage(nil, m))
}

type option struct{}

// Transform returns a cmp.Option that converts each proto.Message to a Message.
// The transformation does not mutate nor alias any converted messages.
//
// The google.protobuf.Any message is automatically unmarshaled such that the
// "value" field is a Message representing the underlying message value
// assuming it could be resolved and properly unmarshaled.
func Transform(...option) cmp.Option {
	// NOTE: There are currently no custom options for Transform,
	// but the use of an unexported type keeps the future open.

	// TODO: Should this transform protoreflect.Enum types to Enum as well?
	return cmp.FilterPath(func(p cmp.Path) bool {
		ps := p.Last()
		if isMessageType(ps.Type()) {
			return true
		}

		// Check whether the concrete values of an interface both satisfy
		// the Message interface.
		if ps.Type().Kind() == reflect.Interface {
			vx, vy := ps.Values()
			if !vx.IsValid() || vx.IsNil() || !vy.IsValid() || vy.IsNil() {
				return false
			}
			return isMessageType(vx.Elem().Type()) && isMessageType(vy.Elem().Type())
		}

		return false
	}, cmp.Transformer("protocmp.Transform", func(v interface{}) Message {
		m := protoimpl.X.MessageOf(v)
		if m == nil || !m.IsValid() {
			return nil
		}
		return transformMessage(m)
	}))
}

func isMessageType(t reflect.Type) bool {
	return t.Implements(messageV1Type) || t.Implements(messageV2Type)
}

func transformMessage(m protoreflect.Message) Message {
	mx := Message{}
	mt := messageType{md: m.Descriptor(), xds: make(map[protoreflect.FullName]protoreflect.FieldDescriptor)}

	// Handle known and extension fields.
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		s := string(fd.Name())
		if fd.IsExtension() {
			s = "[" + string(fd.FullName()) + "]"
			mt.xds[fd.FullName()] = fd
		}
		switch {
		case fd.IsList():
			mx[s] = transformList(fd, v.List())
		case fd.IsMap():
			mx[s] = transformMap(fd, v.Map())
		default:
			mx[s] = transformSingular(fd, v)
		}
		return true
	})

	// Handle unknown fields.
	for b := m.GetUnknown(); len(b) > 0; {
		num, _, n := wire.ConsumeField(b)
		s := strconv.Itoa(int(num))
		b2, _ := mx[s].(protoreflect.RawFields)
		mx[s] = append(b2, b[:n]...)
		b = b[n:]
	}

	// Expand Any messages.
	if mt.md.FullName() == "google.protobuf.Any" {
		// TODO: Expose Transform option to specify a custom resolver?
		s, _ := mx["type_url"].(string)
		b, _ := mx["value"].([]byte)
		mt, err := protoregistry.GlobalTypes.FindMessageByURL(s)
		if mt != nil && err == nil {
			m2 := mt.New()
			err := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal(b, m2.Interface())
			if err == nil {
				mx["value"] = transformMessage(m2)
			}
		}
	}

	mx[messageTypeKey] = mt
	return mx
}

func transformList(fd protoreflect.FieldDescriptor, lv protoreflect.List) interface{} {
	t := protoKindToGoType(fd.Kind())
	rv := reflect.MakeSlice(reflect.SliceOf(t), lv.Len(), lv.Len())
	for i := 0; i < lv.Len(); i++ {
		v := reflect.ValueOf(transformSingular(fd, lv.Get(i)))
		rv.Index(i).Set(v)
	}
	return rv.Interface()
}

func transformMap(fd protoreflect.FieldDescriptor, mv protoreflect.Map) interface{} {
	kfd := fd.MapKey()
	vfd := fd.MapValue()
	kt := protoKindToGoType(kfd.Kind())
	vt := protoKindToGoType(vfd.Kind())
	rv := reflect.MakeMapWithSize(reflect.MapOf(kt, vt), mv.Len())
	mv.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		kv := reflect.ValueOf(transformSingular(kfd, k.Value()))
		vv := reflect.ValueOf(transformSingular(vfd, v))
		rv.SetMapIndex(kv, vv)
		return true
	})
	return rv.Interface()
}

func transformSingular(fd protoreflect.FieldDescriptor, v protoreflect.Value) interface{} {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		return Enum{num: v.Enum(), ed: fd.Enum()}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return transformMessage(v.Message())
	default:
		return v.Interface()
	}
}

func protoKindToGoType(k protoreflect.Kind) reflect.Type {
	switch k {
	case protoreflect.BoolKind:
		return reflect.TypeOf(bool(false))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return reflect.TypeOf(int32(0))
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return reflect.TypeOf(int64(0))
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return reflect.TypeOf(uint32(0))
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return reflect.TypeOf(uint64(0))
	case protoreflect.FloatKind:
		return reflect.TypeOf(float32(0))
	case protoreflect.DoubleKind:
		return reflect.TypeOf(float64(0))
	case protoreflect.StringKind:
		return reflect.TypeOf(string(""))
	case protoreflect.BytesKind:
		return reflect.TypeOf([]byte(nil))
	case protoreflect.EnumKind:
		return reflect.TypeOf(Enum{})
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return reflect.TypeOf(Message{})
	default:
		panic("invalid kind")
	}
}