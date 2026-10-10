package logic

import "reflect"

// isolatedVariables returns a copy of vars that shares no map or slice with
// the original, for binding into a script runtime.
//
// goja binds a Go map or slice by reference: a script that writes
// order.items[0] = … writes straight into the instance's variables. That
// lets a failing script leave half its nested edits behind, lets a gateway
// condition rewrite business data while it is only meant to read it, and —
// worst — lets a script abandoned past its budget keep writing a map the
// engine is concurrently JSON-encoding, which is a fatal runtime error rather
// than a recoverable panic.
func isolatedVariables(vars map[string]any) map[string]any {
	out := make(map[string]any, len(vars)+1)
	for k, v := range vars {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue copies every map and slice reachable from v. Scalars are
// returned as they are; so are pointers, structs and other values a process
// variable decoded from JSON never holds.
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			out[k] = deepCopyValue(e)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = deepCopyValue(e)
		}
		return out
	}

	// Typed maps and slices (map[string]string, []int, []map[string]any …)
	// reach the runtime by reference too, so copy them as well.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), copyElem(iter.Value(), rv.Type().Elem()))
		}
		return out.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := range rv.Len() {
			out.Index(i).Set(copyElem(rv.Index(i), rv.Type().Elem()))
		}
		return out.Interface()
	default:
		return v
	}
}

// copyElem deep-copies one element of a typed map or slice and converts the
// result back to the container's element type.
func copyElem(e reflect.Value, elemType reflect.Type) reflect.Value {
	if !e.CanInterface() {
		return e
	}
	copied := deepCopyValue(e.Interface())
	if copied == nil {
		return reflect.Zero(elemType)
	}
	return reflect.ValueOf(copied).Convert(elemType)
}
