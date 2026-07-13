package common

import (
	"errors"
	"fmt"
	"reflect"
)

const maxNodeInfoDepth = 64

var (
	ErrNodeInfoCycle   = errors.New("node info contains a cycle")
	ErrNodeInfoTooDeep = errors.New("node info exceeds maximum depth")
)

type nodeInfoVisitObj struct {
	kind   reflect.Kind
	typeOf reflect.Type
	ptr    uintptr
}

// CloneNodeInfo recursively copies the JSON-shaped maps and slices accepted by
// Yggdrasil NodeInfo. It rejects cyclic and pathologically deep values instead
// of risking unbounded recursion. Scalar values are immutable and returned as-is.
func CloneNodeInfo(src map[string]any) (map[string]any, error) {
	cloned, err := cloneNodeInfoValue(reflect.ValueOf(src), 0, make(map[nodeInfoVisitObj]struct{}))
	if err != nil {
		return nil, err
	}
	if !cloned.IsValid() || cloned.IsNil() {
		return nil, nil
	}
	return cloned.Interface().(map[string]any), nil
}

func cloneNodeInfoValue(value reflect.Value, depth int, active map[nodeInfoVisitObj]struct{}) (reflect.Value, error) {
	if !value.IsValid() {
		return value, nil
	}
	if depth > maxNodeInfoDepth {
		return reflect.Value{}, fmt.Errorf("%w: limit %d", ErrNodeInfoTooDeep, maxNodeInfoDepth)
	}

	leave, err := enterNodeInfoValue(value, active)
	if err != nil {
		return reflect.Value{}, err
	}
	defer leave()

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		cloned, cloneErr := cloneNodeInfoValue(value.Elem(), depth, active)
		if cloneErr != nil {
			return reflect.Value{}, cloneErr
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(cloned)
		return out, nil
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned, cloneErr := cloneNodeInfoValue(iter.Value(), depth+1, active)
			if cloneErr != nil {
				return reflect.Value{}, cloneErr
			}
			out.SetMapIndex(iter.Key(), cloned)
		}
		return out, nil
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := range value.Len() {
			cloned, cloneErr := cloneNodeInfoValue(value.Index(i), depth+1, active)
			if cloneErr != nil {
				return reflect.Value{}, cloneErr
			}
			out.Index(i).Set(cloned)
		}
		return out, nil
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			cloned, cloneErr := cloneNodeInfoValue(value.Index(i), depth+1, active)
			if cloneErr != nil {
				return reflect.Value{}, cloneErr
			}
			out.Index(i).Set(cloned)
		}
		return out, nil
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		cloned, cloneErr := cloneNodeInfoValue(value.Elem(), depth+1, active)
		if cloneErr != nil {
			return reflect.Value{}, cloneErr
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloned)
		return out, nil
	default:
		return value, nil
	}
}

func enterNodeInfoValue(value reflect.Value, active map[nodeInfoVisitObj]struct{}) (func(), error) {
	switch value.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer:
		if value.IsNil() {
			return func() {}, nil
		}
	default:
		return func() {}, nil
	}
	ptr := uintptr(value.UnsafePointer())
	if ptr == 0 {
		return func() {}, nil
	}
	visit := nodeInfoVisitObj{kind: value.Kind(), typeOf: value.Type(), ptr: ptr}
	if _, exists := active[visit]; exists {
		return nil, fmt.Errorf("%w at %s", ErrNodeInfoCycle, value.Type())
	}
	active[visit] = struct{}{}
	return func() { delete(active, visit) }, nil
}
