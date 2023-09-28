package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func Unmarshal(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return NotAPointerToStruct{}
	}

	return unmarshal(rv.Elem())
}

type tagInfo struct {
	ArgType  ArgType
	Required bool
	Default  reflect.Value
	Help     string
	Flags    []string
	Nargs    int
}

func (t *tagInfo) String() string {
	if t.ArgType == Arg {
		return fmt.Sprintf("required:%v, default:%v", t.Required, t.Default.Interface())
	}
	var defaultValue interface{}
	if t.Default.Kind() != reflect.Invalid {
		defaultValue = t.Default.Interface()
	}
	return fmt.Sprintf(
		"required:%v, default:%v, flags:%v, nargs:%d",
		t.Required,
		defaultValue,
		t.Flags,
		t.Nargs,
	)
}

type field struct {
	Value        *reflect.Value
	Type         reflect.Type
	TagInfo      *tagInfo
	StructOffset int
}

type llArgs struct {
	Val  string
	Next *llArgs
	Prev *llArgs
}

func (ll *llArgs) Len() int {
	next := ll
	res := 0
	for next != nil {
		res++
		next = next.Next
	}
	return res
}

func (ll *llArgs) String() string {
	next := ll
	res := make([]string, ll.Len())
	for i := 0; i < len(res); i++ {
		res[i] = next.Val
		next = next.Next
	}

	return strings.Join(res, " ")
}

func newLLArgs(args []string) *llArgs {
	res := new(llArgs)
	next := res

	for index := 0; index < len(args); index++ {
		next.Next = new(llArgs)
		next.Next.Prev = next
		next = next.Next
		next.Val = args[index]
	}

	res.Next.Prev = nil
	return res.Next
}

func unmarshal(rv reflect.Value) error {
	var (
		err    error
		fields []*field
	)

	fields, err = parseFields(rv)
	if err != nil {
		return err
	}

	// move args to end
	for i := 0; i < len(fields); i++ {
		for j := 0; j < len(fields); j++ {
			if i == j {
				continue
			}

			if i < j && fields[i].TagInfo.ArgType == Arg {
				// preserver args position
				if fields[j].TagInfo.ArgType == Arg && fields[i].StructOffset < fields[j].StructOffset {
					continue
				}
				fields[i], fields[j] = fields[j], fields[i]
			}
		}
	}

	required := 0
	for _, f := range fields {
		if f.TagInfo.Required {
			required++
		}
	}

	args := newLLArgs(os.Args[1:])

	if required > args.Len() {
		return &NotEnoughRequiredArgs{
			Expected: required,
			Actual:   args.Len(),
		}
	}

	for index := 1; index < len(os.Args); index++ {
		if os.Args[index] == "--help" || os.Args[index] == "-h" {
			return &HelpMessage{
				Message: getHelpMessage(fields),
			}
		}
	}

	for _, f := range fields {
		f.Value = new(reflect.Value)
		if err = parseArg(f, args); err != nil {
			return err
		}

		rvField := rv.Field(f.StructOffset)
		rvField.Set(f.Value.Convert(rvField.Type()))
	}

	return nil
}

func parseArg(f *field, args *llArgs) error {
	next := args
	removeFromLL := func(current *llArgs) {
		if current.Next == nil && current.Prev == nil {
			return
		}

		if current.Prev == nil {
			current.Next.Prev = nil
			*args = *current.Next
		} else {
			*current.Prev.Next = *current.Next
			*current.Next.Prev = *current.Prev
		}
	}

	for next != nil {
		isFlag := strings.HasPrefix(next.Val, "-")
		prevIsFlag := false
		if next.Prev != nil {
			prevIsFlag = strings.HasPrefix(next.Prev.Val, "-")
		}

		switch {
		case f.TagInfo.ArgType == Arg && !isFlag && !prevIsFlag:
			val, err := parseStringValue(f.Type, next.Val)
			if err != nil {
				return &WrongArgValue{
					ErrorDesc: err.Error(),
				}
			}
			*f.Value = val
			removeFromLL(next)
			return nil

		case f.TagInfo.ArgType == Option && isFlag && slices.Contains[[]string, string](f.TagInfo.Flags, next.Val):
			removeFromLL(next)

			if f.Type.Kind() != reflect.Array && f.Type.Kind() != reflect.Slice {
				val, err := parseStringValue(f.Type, next.Val)
				if err != nil {
					return &WrongArgValue{
						ErrorDesc: err.Error(),
					}
				}
				removeFromLL(next)

				*f.Value = val
				return nil
			}

			additionalTest := func(str string) bool {
				if f.Type.Elem().Kind() != reflect.String {
					return !strings.HasPrefix(str, "-")
				}
				return true
			}

			switch f.Type.Kind() {
			case reflect.Array:
				// TODO:
			case reflect.Slice:
				count := 0
				vals := reflect.MakeSlice(f.Type, f.TagInfo.Nargs, f.TagInfo.Nargs)
				for next != nil && additionalTest(next.Val) && count < f.TagInfo.Nargs {
					val, err := parseStringValue(f.Type.Elem(), next.Val)
					if err != nil {
						return &WrongArgValue{
							ErrorDesc: err.Error(),
						}
					}
					vals.Index(count).Set(val)
					count++
					removeFromLL(next)
				}

				if count < f.TagInfo.Nargs {
					return &WrongArgValue{
						ErrorDesc: fmt.Sprintf("not enougth option values, required: %d, passed %d"),
					}
				}
				*f.Value = vals
			}

			return nil
		case f.TagInfo.ArgType == Flag && isFlag && slices.Contains[[]string, string](f.TagInfo.Flags, next.Val):
			*f.Value = reflect.ValueOf(true)
			removeFromLL(next)
			return nil
		}

		next = next.Next
	}

	if f.TagInfo.Required {
		return &ArgNotFound{ArgType: f.TagInfo.ArgType, Extra: f.TagInfo.String()}
	}

	if f.TagInfo.Default.Kind() != reflect.Invalid {
		*f.Value = f.TagInfo.Default
		return nil
	}

	*f.Value = reflect.Zero(f.Type)
	return nil
}

func getHelpMessage(fields []*field) string {
	res := make([]string, len(fields))
	for i, f := range fields {
		res[i] = f.TagInfo.Help
	}

	return strings.Join(res, "\n")
}

func parseFields(rv reflect.Value) ([]*field, error) {
	fields := make([]*field, len(reflect.VisibleFields(rv.Type())))

	for index, f := range reflect.VisibleFields(rv.Type()) {
		fv, err := processField(f, index)
		if err != nil {
			return nil, err
		}

		fields[index] = fv
	}

	return fields, nil
}

func processField(f reflect.StructField, offset int) (*field, error) {
	tf, err := processTags(f)
	if err != nil {
		return nil, err
	}

	rf := new(field)
	rf.Type = f.Type
	rf.StructOffset = offset
	rf.TagInfo = tf

	return rf, nil
}

type TagName string

const (
	mainTagName    TagName = "argparser"
	helpTagName            = mainTagName + "_help"
	defaultTagName         = mainTagName + "_default"
)

type ArgType string

const (
	Arg    = "arg"
	Option = "option"
	Flag   = "flag"
)

func processTags(f reflect.StructField) (*tagInfo, error) {
	info := new(tagInfo)

	mainTagValue, ok := f.Tag.Lookup(string(mainTagName))
	if !ok {
		return nil, &MissingMainTag{Key: f.Name}
	}
	if mainTagValue == "" {
		return nil, &EmptyTag{FieldName: f.Name, TagName: mainTagValue}
	}

	var tagType string
	params := strings.Split(mainTagValue, ",")
	switch params[0] {
	case Arg, Option, Flag:
		info.ArgType = ArgType(params[0])
	default:
		return nil, &UnknownMainTagType{FieldName: f.Name, Type: params[0]}
	}

	if info.ArgType == Flag && f.Type.Kind() != reflect.Bool {
		return nil, &NotValidTag{
			FieldName: f.Name,
			TagName:   mainTagName,
			ErrorDesc: "flag typed field must be bool",
		}
	}

	if info.ArgType != Arg && !strings.HasPrefix(params[1], "-") {
		return nil, &NotValidTag{
			FieldName: f.Name,
			TagName:   mainTagName,
			ErrorDesc: fmt.Sprintf("%s params must start with flags (like -name or --name)", tagType),
		}
	}

	if info.ArgType == Flag {
		info.Default = reflect.ValueOf(false)
	}

	for index := 1; index < len(params); index++ {
		switch {
		case info.ArgType != Arg && strings.HasPrefix(params[index], "-"):
			info.Flags = append(info.Flags, params[index])

		case info.ArgType != Flag && params[index] == "required":
			info.Required = true

		case info.ArgType == Option && strings.HasPrefix(params[index], "nargs="):
			nargsValStr := params[index][len("nargs="):]
			nargs, err := strconv.Atoi(nargsValStr)
			if err != nil {
				return nil, &NotValidTag{
					FieldName: f.Name,
					TagName:   mainTagName,
					ErrorDesc: fmt.Sprintf("error while parsing nargs value: %s", nargsValStr),
				}
			}

			if nargs > 1 && f.Type.Kind() != reflect.Array && f.Type.Kind() != reflect.Slice {
				return nil, &NotValidTag{
					FieldName: f.Name,
					TagName:   mainTagName,
					ErrorDesc: fmt.Sprintf("not valid type for nargs>1: %v", f.Type),
				}
			}

			if f.Type.Kind() == reflect.Array && reflect.Zero(f.Type).Cap() < nargs {
				return nil, &NotValidTag{
					FieldName: f.Name,
					TagName:   mainTagName,
					ErrorDesc: "value ont nargs greater then capacity of field",
				}
			}

			info.Nargs = nargs

		default:
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   mainTagName,
				ErrorDesc: fmt.Sprintf("unknown param `%s` for %s tag type", params[index], tagType),
			}
		}
	}

	if helpTagValue, ok := f.Tag.Lookup(string(helpTagName)); ok {
		info.Help = helpTagValue
	}

	if defaultTagValue, ok := f.Tag.Lookup(string(defaultTagName)); ok {
		if info.ArgType == Arg {
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   defaultTagName,
				ErrorDesc: "arg typed field can not have default value",
			}
		}

		val, err := parseStringValue(f.Type, defaultTagValue)
		if err != nil {
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   defaultTagName,
				ErrorDesc: err.Error(),
			}
		}

		if info.ArgType == Flag && val.Kind() != reflect.Bool {
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   defaultTagName,
				ErrorDesc: "default value of flag typed field must be bool",
			}
		}

		info.Default = val
	}

	return info, nil
}

func parseStringValue(ft reflect.Type, s string) (reflect.Value, error) {
	var parseFunc func(string) (reflect.Value, error)

	switch ft.Kind() {
	case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		parseFunc = func(str string) (reflect.Value, error) {
			data := reflect.Zero(ft).Interface()
			if err := json.Unmarshal([]byte(s), &data); err != nil {
				return reflect.Value{}, err
			}

			res, err := postJson(ft, data)
			if err != nil {
				return reflect.Value{}, err
			}

			return res, nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parseFunc = func(str string) (reflect.Value, error) {
			val, err := strconv.ParseInt(str, 10, 64)
			if err != nil {
				return reflect.Value{}, err
			}
			if reflect.Zero(ft).OverflowInt(val) {
				return reflect.Value{}, errors.New("int value bigger then field type")
			}
			return reflect.ValueOf(val), nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parseFunc = func(str string) (reflect.Value, error) {
			val, err := strconv.ParseUint(str, 10, 64)
			if err != nil {
				return reflect.Value{}, err
			}
			if reflect.Zero(ft).OverflowUint(val) {
				return reflect.Value{}, errors.New("uint value bigger then field type")
			}
			return reflect.ValueOf(val), nil
		}
	case reflect.Float32, reflect.Float64:
		parseFunc = func(str string) (reflect.Value, error) {
			val, err := strconv.ParseFloat(str, 64)
			if err != nil {
				return reflect.Value{}, err
			}
			if reflect.Zero(ft).OverflowFloat(val) {
				return reflect.Value{}, errors.New("float value bigger then field type")
			}
			return reflect.ValueOf(val), nil
		}
	case reflect.Bool:
		parseFunc = func(str string) (reflect.Value, error) {
			val, err := strconv.ParseBool(str)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(val), nil
		}
	case reflect.String:
		parseFunc = func(str string) (reflect.Value, error) {
			return reflect.ValueOf(str), nil
		}
	default:
		parseFunc = func(str string) (reflect.Value, error) {
			return reflect.Value{}, errors.New(fmt.Sprintf("unsupported type %s of default value %s", ft, str))
		}
	}

	return parseFunc(s)
}

func postJson(vt reflect.Type, data any) (reflect.Value, error) {
	switch vt.Kind() {
	case reflect.Array:
		// TODO:
	case reflect.Slice:
		dv := reflect.ValueOf(data)
		res := reflect.MakeSlice(vt, dv.Len(), dv.Len())
		for i := 0; i < dv.Len(); i++ {
			val, err := postJson(vt.Elem(), dv.Index(i).Interface())
			if err != nil {
				return reflect.Value{}, err
			}
			res.Index(i).Set(val)
		}
		return res, nil
	case reflect.Map:
		dv := reflect.ValueOf(data)
		res := reflect.MakeMapWithSize(vt, dv.Len())
		iter := dv.MapRange()
		for iter.Next() {
			k, v := iter.Key(), iter.Value()
			kv, err := postJson(vt.Key(), k.Interface())
			if err != nil {
				return reflect.Value{}, err
			}
			vv, err := postJson(vt.Elem(), v.Interface())
			if err != nil {
				return reflect.Value{}, err
			}
			res.SetMapIndex(kv, vv)
		}
		return res, nil
	case reflect.Struct:
		dv := reflect.ValueOf(data)
		if dv.Kind() != reflect.Map || dv.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, &WrongValueType{
				Expected: "map[string]any",
				Actual:   dv.Type().String(),
			}
		}

		structKeys := make(map[string]struct{})

		for _, sf := range reflect.VisibleFields(vt) {
			structKeys[sf.Name] = struct{}{}
		}

		res := reflect.New(vt)
		iter := dv.MapRange()
		for iter.Next() {
			k := iter.Key().Interface().(string)
			if _, ok := structKeys[strings.ToTitle(k)]; ok {
				f := res.Elem().FieldByName(strings.ToTitle(k))
				val, err := postJson(f.Type(), iter.Value().Interface())
				if err != nil {
					return reflect.Value{}, err
				}
				f.Set(val)
			}
		}
		return res.Elem(), nil
	default:
		if !reflect.ValueOf(data).CanConvert(vt) {
			return reflect.Value{}, &WrongValueType{
				Expected: vt.String(),
				Actual:   reflect.ValueOf(data).Type().String(),
			}
		}
		return reflect.ValueOf(data).Convert(vt), nil
	}

	return reflect.ValueOf(data), nil
}
