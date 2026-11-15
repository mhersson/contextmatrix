package config

import (
	"reflect"
	"strings"
)

// fillTagged sets every string reachable from v whose yaml tag name accepts to
// value(n), n counting from 1, allocating pointers, slice elements and map
// entries on the way, and returns the values it set.
func fillTagged(v reflect.Value, accepts func(name string) bool, value func(n int) string) []string {
	var set []string

	var fill func(v reflect.Value)

	fill = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}

			fill(v.Elem())
		case reflect.Struct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() {
					continue
				}

				fv := v.Field(i)
				if name, _, _ := strings.Cut(f.Tag.Get("yaml"), ","); fv.Kind() == reflect.String && accepts(name) {
					s := value(len(set) + 1)
					fv.SetString(s)
					set = append(set, s)

					continue
				}

				fill(fv)
			}
		case reflect.Slice:
			if v.Type().Elem().Kind() == reflect.String {
				return
			}

			v.Set(reflect.MakeSlice(v.Type(), 2, 2))

			for i := range v.Len() {
				fill(v.Index(i))
			}
		case reflect.Map:
			if v.Type().Key().Kind() != reflect.String {
				return
			}

			v.Set(reflect.MakeMap(v.Type()))

			e := reflect.New(v.Type().Elem()).Elem()
			fill(e)
			v.SetMapIndex(reflect.ValueOf("k").Convert(v.Type().Key()), e)
		}
	}

	fill(v)

	return set
}
